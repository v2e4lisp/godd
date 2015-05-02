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
        procNamePat = regexp.MustCompile("^[a-zA-z0-9_]+$")
        envNamePat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
        timefmt     = "15:04:05"
        cfmt        = "%s"

        // command options
        procfile string
        envfile  string
        wd       string
)

type Command struct {
        name string
        c    *exec.Cmd
}

func log(c *Command, msg ...interface{}) {
        t := time.Now().Local().Format(timefmt)
        name := fmt.Sprintf(cfmt, c.name)
        s := append([]interface{}{t, name, "|"}, msg...)
        fmt.Println(s...)
}

func logging(c *Command) error {
        stdout, err := c.c.StdoutPipe()
        if err != nil {
                return err
        }
        stderr, err := c.c.StderrPipe()
        if err != nil {
                return err
        }
        bufout, buferr := bufio.NewReader(stdout), bufio.NewReader(stderr)
        p := func(b *bufio.Reader) {
                for {
                        line, err := b.ReadBytes('\n')
                        if err != nil {
                                break
                        }
                        log(c, strings.TrimSpace(string(line)))
                }
        }

        go p(bufout)
        go p(buferr)
        return nil
}

func loadEnv(envfile string) ([]string, error) {
        env := []string(nil)
        text, err := ioutil.ReadFile(envfile)
        if err != nil {
                return nil, err
        }
        lines := strings.Split(string(text), "\n")

        for ln, l := range lines {
                if l == "" || l[0] == '#' {
                        continue
                }

                e := strings.SplitN(l, "=", 2)
                if len(e) != 2 || !envNamePat.Match([]byte(e[0])) {
                        msg := fmt.Sprintf("parsing error at %s#%d:\n\t%s",
                                envfile, ln+1, l)
                        return nil, errors.New(msg)
                }

                key := e[0]
                val := strings.TrimSpace(e[1])
                last := len(val) - 1
                if val[0] == '\'' && val[last] == '\'' {
                        val = val[1:last]
                } else if val[0] == '"' && val[last] == '"' {
                        val = val[1:last]
                        val = strings.Replace(val, "\\\"", "\"", -1)
                        val = strings.Replace(val, "\\n", "\n", -1)
                }

                env = append(env, key+"="+val)
        }

        return env, nil
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
                if len(p) != 2 || !procNamePat.Match([]byte(p[0])) {
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
                if err := logging(cmd); err != nil {
                        log(cmd, "unable to redirect stderr and stdout", err)
                }
                // If you get this error, chances are the `sh' is not found
                if err := cmd.c.Start(); err != nil {
                        log(cmd, err)
                        wg.Add(i - len(cmds))
                        done <- true
                        break
                }
                log(cmd, "STARTED", "PID:", cmd.c.Process.Pid)

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
                                cmd.c.Process.Signal(syscall.SIGTERM)
                                // if SIGTERM cannot kill the process
                                // send it a SIGKILL
                                select {
                                case <-exit:
                                        log(cmd, "KILLED BY SIGTERM")
                                case <-time.After(3 * time.Second):
                                        cmd.c.Process.Kill()
                                        log(cmd, "KILLED BY SIGKILL")
                                }
                        case code := <-exit:
                                // `done' is a buffered channel
                                // sending msg to `done' does not block
                                if code == nil {
                                        log(cmd, "EXITED: exit status 0")
                                } else {
                                        log(cmd, "EXITED:", code)
                                }
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
  godd run [COMMAND]     # Load the dot env file and run any command.
  godd version           # Show version

OPTIONS:
`)
                flag.PrintDefaults()
        }
        flag.StringVar(&procfile, "p", "Procfile", "specify Procfile")
        flag.StringVar(&envfile, "e", ".env", "specify dot env file")
        flag.StringVar(&wd, "d", ".", "specify working dir")
        flag.Parse()

        var err error
        procfile, err = filepath.Abs(procfile)
        if err != nil {
                abort("Procfile error:", err.Error())
        }
        envfile, err = filepath.Abs(envfile)
        if err != nil {
                abort("Env file error:", err.Error())
        }
        wd, err = filepath.Abs(wd)
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
        case "run":
                doRun()
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
        env, err := loadEnv(envfile)
        if err != nil {
                abort("Env file error:", err)
        }

        cmds := []*Command(nil)
        maxlen := 0
        for name, p := range procs {
                if proc == "" || proc == name {
                        if len(name) > maxlen {
                                maxlen = len(name)
                        }

                        c := exec.Command("sh", "-c", p)
                        c.Dir = wd
                        c.Env = env
                        cmd := &Command{
                                name: name,
                                c:    c,
                        }
                        cmds = append(cmds, cmd)
                }
        }

        if len(procs) > 0 && len(cmds) == 0 && proc != "" {
                abort("Proc not found:", proc)
        }

        cfmt = "%-" + fmt.Sprintf("%d", maxlen) + "s"
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

func doRun() {
        if flag.NArg() != 2 {
                flag.Usage()
                os.Exit(1)
        }

        env, err := loadEnv(envfile)
        if err != nil {
                abort("Env file error:", err)
        }
        cmd := flag.Arg(1)
        c := exec.Command("sh", "-c", cmd)
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        c.Dir = wd
        c.Env = env
        c.Run()
}
