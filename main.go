package main

import (
        "errors"
        "flag"
        "fmt"
        "io/ioutil"
        "os"
        "os/exec"
        "os/signal"
        "path/filepath"
        "regexp"
        "strings"
        "sync"
        "syscall"
)

var (
        procpat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
        procfile = "Procfile"
)

func loadProcs(procfile string) (map[string]string, error) {
        procs := make(map[string]string)
        text, err := ioutil.ReadFile(procfile)
        if err != nil {
                return nil, err
        }
        lines := strings.Split(string(text), "\n")

        for ln, l := range lines {
                if l == "" || l[0] == '#' {
                        continue
                }
                p := strings.SplitN(l, ":", 2)
                if len(p) != 2 || !procpat.Match([]byte(p[0])) {
                        msg := fmt.Sprintf("parsing error at %s#%d:\n\t%s",
                                procfile, ln+1, l)
                        return nil, errors.New(msg)
                }
                procs[p[0]] = strings.TrimSpace(p[1])
        }

        return procs, nil
}

func run(cmds []*exec.Cmd) {
        // broadcast to kill all commands' processes
        kill := make(chan bool)
        // any command finished
        done := make(chan bool)
        // handle Ctrl-C and other signal
        sigs := make(chan os.Signal, 1)

        signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

        var wg sync.WaitGroup
        for _, cmd := range cmds {
                cmd.Stdout = os.Stdout
                cmd.Stderr = os.Stderr

                exit := make(chan error)
                err := cmd.Start()
                if err != nil {
                        fmt.Println(err)
                        done <- true
                        break
                }
                wg.Add(1)

                go func(cmd *exec.Cmd, exit chan error) {
                        exit <- cmd.Wait()
                }(cmd, exit)

                // If the command exists, send a message to `done' channel
                go func(cmd *exec.Cmd, exit chan error) {
                        defer wg.Done()
                        select {
                        case <-kill:
                                if err := cmd.Process.Kill(); err != nil {
                                        fmt.Println(err)
                                }
                        case <-exit:
                                done <- true
                        }
                }(cmd, exit)
        }

        select {
        case <-done:
                close(kill)
        case <-sigs:
                close(kill)
        }

        wg.Wait()
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

// subcommand
// godd start [process]
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

// subcommand
// godd check
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
