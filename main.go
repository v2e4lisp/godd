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

var (
        procpat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
        timefmt  = "2006-01-02 15:04:05"
        procfile = "Procfile"
)

type Command struct {
        name string
        c    *exec.Cmd
}

func (c *Command) puts(msg ...interface{}) {
        t := time.Now().Local().Format(timefmt)
        s := append([]interface{}{"[" + t + "|" + c.name + "]"}, msg...)
        fmt.Println(s...)
}

type CommandLogger struct {
        cmd    *Command
        bufout *bufio.Reader
        buferr *bufio.Reader
}

func NewCommandLogger(c *Command) (l *CommandLogger, err error) {
        stdout, err := c.c.StdoutPipe()
        if err != nil {
                return nil, err
        }
        stderr, err := c.c.StderrPipe()
        if err != nil {
                return nil, err
        }

        bufout, buferr := bufio.NewReader(stdout), bufio.NewReader(stderr)
        return &CommandLogger{c, bufout, buferr}, nil
}

func (l *CommandLogger) start() {
        go func() {
                for {
                        line, err := l.bufout.ReadBytes('\n')
                        if err != nil {
                                // l.cmd.puts(err)
                                break
                        }
                        l.cmd.puts(strings.TrimSpace(string(line)))
                }
        }()

        go func() {
                for {
                        line, err := l.buferr.ReadBytes('\n')
                        if err != nil {
                                // l.cmd.puts(err)
                                break
                        }
                        l.cmd.puts(strings.TrimSpace(string(line)))
                }
        }()
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
        // handle Ctrl-C and other signal
        sigs := make(chan os.Signal, 1)

        signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

        var wg sync.WaitGroup
        wg.Add(len(cmds))
        for i, cmd := range cmds {
                l, err := NewCommandLogger(cmd)
                if err != nil {
                        fmt.Println(cmd.name, "unable to initialize logger", err)
                }
                l.start()

                // If you get this error, chances are the `sh' is not found
                if err := cmd.c.Start(); err != nil {
                        fmt.Println(err)
                        wg.Add(i - len(cmds))
                        done <- true
                        break
                }
                cmd.puts("STARTED")

                exit := make(chan error)
                go func(cmd *Command, exit chan error) {
                        exit <- cmd.c.Wait()
                }(cmd, exit)

                // To prevent killing a terminated command,
                // send a message to the `done' channel and exit the goroutine
                // if the command is finished
                go func(cmd *Command, exit chan error) {
                        defer wg.Done()
                        select {
                        case <-kill:
                                // for commands that failed to Start
                                if cmd.c.Process == nil {
                                        break
                                }
                                err := cmd.c.Process.Signal(syscall.SIGTERM)
                                if err != nil {
                                        cmd.puts(err)
                                }
                                cmd.puts("KILLED")
                        case code := <-exit:
                                // `done' is a buffered channel
                                // sending msg to `done' do not block
                                done <- true
                                cmd.puts("EXITED", code)
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

        cmds := []*Command(nil)
        for name, c := range procs {
                if proc == "" || proc == name {
                        cmd := &Command{name, exec.Command("sh", "-c", c)}
                        cmds = append(cmds, cmd)
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
