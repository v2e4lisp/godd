package main

import (
        "bufio"
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
        "time"
)

const VERSION = "0.1.0"

var (
        procpat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
        timefmt  = "2006-01-02 15:04:05"
        procfile = "Procfile"
)

type Command struct {
        name string
        c    *exec.Cmd
}

func (c *Command) log(msg ...interface{}) {
        t := time.Now().Local().Format(timefmt)
        s := append([]interface{}{"[" + t + "|" + c.name + "]"}, msg...)
        fmt.Println(s...)
}

func (c *Command) logging() error {
        stdout, err := c.c.StdoutPipe()
        if err != nil {
                return err
        }
        stderr, err := c.c.StderrPipe()
        if err != nil {
                return err
        }
        bufout, buferr := bufio.NewReader(stdout), bufio.NewReader(stderr)

        go func() {
                for {
                        line, err := bufout.ReadBytes('\n')
                        if err != nil {
                                break
                        }
                        c.log(strings.TrimSpace(string(line)))
                }
        }()

        go func() {
                for {
                        line, err := buferr.ReadBytes('\n')
                        if err != nil {
                                break
                        }
                        c.log(strings.TrimSpace(string(line)))
                }
        }()

        return nil
}

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

func run(cmds []*Command) {
        // broadcast to kill all commands' processes
        kill := make(chan bool)
        // any command finished
        done := make(chan bool, len(cmds))
        // handle Ctrl-C and other signals
        sigs := make(chan os.Signal, 1)

        signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

        var wg sync.WaitGroup
        wg.Add(len(cmds))
        for i, cmd := range cmds {
                if err := cmd.logging(); err != nil {
                        cmd.log(cmd.name, "unable to redirect stderr and stdout", err)
                }

                // If you get this error, chances are the `sh' is not found
                if err := cmd.c.Start(); err != nil {
                        cmd.log(err)
                        wg.Add(i - len(cmds))
                        done <- true
                        break
                }
                cmd.log("STARTED")

                exit := make(chan error)
                go func(cmd *Command, exit chan error) {
                        exit <- cmd.c.Wait()
                }(cmd, exit)

                // To prevent killing a terminated command,
                // send a message to the `done' channel and exit the goroutine
                go func(cmd *Command, exit chan error) {
                        defer wg.Done()
                        select {
                        case <-kill:
                                // for commands that failed to Start
                                if cmd.c.Process == nil {
                                        break
                                }
                                cmd.c.Process.Signal(syscall.SIGTERM)
                                // if SIGTERM cannot kill the process
                                // send it a SIGKILL
                                select {
                                case <-exit:
                                        cmd.log("KILLED BY SIGTERM")
                                case <-time.After(3 * time.Second):
                                        cmd.c.Process.Kill()
                                        cmd.log("KILLED BY SIGKILL")
                                }
                        case code := <-exit:
                                // `done' is a buffered channel
                                // sending msg to `done' does not block
                                cmd.log("EXITED", code)
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
        flag.Usage = func() {
                fmt.Print(`COMMANDS:
  godd check             # Validate Procfile
  godd start [PROCESS]   # Start all processes(or a specific PROCESS)
  godd version           # Show version

OPTIONS:
`)
                flag.PrintDefaults()
        }
        flag.StringVar(&procfile, "p", "Procfile", "specify Procfile")
        flag.Parse()

        var err error
        procfile, err = filepath.Abs(procfile)
        if err != nil {
                abort("Procfile error:", err.Error())
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
        case "version":
                fmt.Println("godd", VERSION)
        default:
                abort("Command not found:", flag.Arg(0))
        }
}

func doStart() {
        proc := ""
        if flag.NArg() > 2 {
                flag.Usage()
                os.Exit(1)
        }
        if flag.NArg() == 2 {
                proc = flag.Arg(1)
        }

        procs, err := loadProcs(procfile)
        if err != nil {
                abort("Procfile error:", err)
        }

        cmds := []*Command(nil)
        for name, c := range procs {
                if proc == "" || proc == name {
                        cmd := &Command{
                                name: name,
                                c:    exec.Command("sh", "-c", c),
                        }
                        cmds = append(cmds, cmd)
                }
        }

        if len(procs) > 0 && len(cmds) == 0 && proc != "" {
                abort("Proc not found:", proc)
        }

        run(cmds)
}

func doCheck() {
        if flag.NArg() > 1 {
                flag.Usage()
                os.Exit(1)
        }

        _, err := loadProcs(procfile)
        if err != nil {
                abort("Procfile error:", err)
        }
        fmt.Println("OK")
}
