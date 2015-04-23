package main

import (
        "bufio"
        "errors"
        "flag"
        "fmt"
        "os"
        "os/exec"
        "os/signal"
        "regexp"
        "strings"
        "sync"
)

var (
        procpat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
        procfile = "Procfile"
        envfile  = ".env"
        procs    = make(map[string]string)
        cmds     = []*exec.Cmd(nil)
        start    string
)

func loadProcs() error {
        file, err := os.Open("Procfile")
        if err != nil {
                return err
        }
        defer file.Close()

        s := bufio.NewScanner(file)
        for s.Scan() {
                l := strings.TrimSpace(s.Text())
                if l == "" || l[0] == '#' {
                        continue
                }
                p := strings.SplitN(l, ":", 2)
                if len(p) != 2 || !procpat.Match([]byte(p[0])) {
                        return errors.New("malformat process: " + l)
                }
                procs[p[0]] = strings.TrimSpace(p[1])
        }

        return nil
}

func loadEnv() error {
        // pass
        return nil
}

func run() {
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

func main() {
        flag.Parse()
        if flag.NArg() < 1 {
                fmt.Println("godd subcomand ...")
                os.Exit(1)
        }
        args := flag.Args()
        switch args[0] {
        case "start":
                if len(args) > 2 {
                        fmt.Println("godd start [process]")
                        os.Exit(1)
                }
                if len(args) == 2 {
                        start = args[1]
                }
        }

        if err := loadProcs(); err != nil {
                fmt.Println(err)
                os.Exit(1)
        }

        if err := loadEnv(); err != nil {
                fmt.Println(err)
                os.Exit(1)
        }

        for name, cmd := range procs {
                c := strings.Split(cmd, " ")
                if start == "" || start == name {
                        cmds = append(cmds, exec.Command(c[0], c[1:]...))
                }
        }

        run()
}
