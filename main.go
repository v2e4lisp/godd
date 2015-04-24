package main

import (
        "bufio"
        "errors"
        "flag"
        "fmt"
        "os"
        "os/exec"
        "os/signal"
        "path/filepath"
        "regexp"
        "strings"
        "sync"
)

var (
        procpat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
        procfile = "Procfile"
)

// type Command struct {
//         cmd  *exec.Cmd
//         name string
// }

func loadProcs(procfile string) (map[string]string, error) {
        file, err := os.Open(procfile)
        if err != nil {
                return nil, err
        }
        defer file.Close()

        procs := make(map[string]string)
        s := bufio.NewScanner(file)
        ln := 0
        for s.Scan() {
                ln++
                l := strings.TrimSpace(s.Text())
                if l == "" || l[0] == '#' {
                        continue
                }
                p := strings.SplitN(l, ":", 2)
                if len(p) != 2 || !procpat.Match([]byte(p[0])) {
                        msg := fmt.Sprintf("parsing error at %s#%d:\n\t%s", procfile, ln, l)
                        return nil, errors.New(msg)
                }
                procs[p[0]] = strings.TrimSpace(p[1])
        }

        return procs, nil
}

func run(cmds []*exec.Cmd) {
        // broadcast to kill all commands' processes
        kill := make(chan bool)
        // all commands finished
        done := make(chan bool)
        // handle Ctrl-C and other signal
        sigs := make(chan os.Signal, 5)

        var wg sync.WaitGroup
        wg.Add(len(cmds))
        go func() {
                wg.Wait()
                done <- true
        }()

        signal.Notify(sigs, os.Interrupt, os.Kill)

        for _, cmd := range cmds {
                cmd.Stdout = os.Stdout
                cmd.Stderr = os.Stderr
                go func(cmd *exec.Cmd) {
                        defer wg.Done()
                        done := make(chan bool)
                        err := cmd.Run()
                        if err != nil {
                                fmt.Println(err)
                                return
                        }

                        go func() {
                                cmd.Wait()
                                done <- true
                        }()

                        select {
                        case <-done:
                        case <-kill:
                                if err := cmd.Process.Kill(); err != nil {
                                        fmt.Println(err)
                                }
                        }
                }(cmd)
        }

        select {
        case <-done:
        case <-sigs:
                close(kill)
        }
}

func abort(msg ...interface{}) {
        fmt.Println(msg...)
        os.Exit(1)
}

func main() {
        flag.StringVar(&procfile, "procfile", "Procfile", "specify Procfile")
        flag.Parse()

        var err error
        procfile, err = filepath.Abs(procfile)
        if err != nil {
                abort("Procfile", procfile, err.Error())
        }

        if flag.NArg() < 1 {
                flag.Usage()
                os.Exit(1)
        }

        switch flag.Arg(0) {
        case "start":
                doStart()
        case "check":
                doCheck()
        default:
                abort("command not found:", flag.Arg(0))
        }
}

func doStart() {
        proc := ""
        if flag.NArg() > 2 {
                abort("godd [-p /path/to/procfile] start [proc]")
        }
        if flag.NArg() == 2 {
                proc = flag.Arg(1)
        }

        procs, err := loadProcs(procfile)
        if err != nil {
                abort("Procfile", err)
        }

        cmds := []*exec.Cmd(nil)
        for name, cmd := range procs {
                c := strings.Split(cmd, " ")
                if proc == "" || proc == name {
                        cmds = append(cmds, exec.Command(c[0], c[1:]...))
                }
        }

        if len(procs) > 0 && len(cmds) == 0 && proc != "" {
                abort("proc not found:", proc)
        }

        run(cmds)
}

func doCheck() {
        if flag.NArg() > 1 {
                abort("godd [-p /path/to/procfile] check")
        }

        _, err := loadProcs(procfile)
        if err != nil {
                abort("Procfile", err)
        }
        fmt.Println("OK")
}
