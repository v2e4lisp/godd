package main

import (
        "bufio"
        "errors"
        "fmt"
        "io/ioutil"
        "os"
        "os/exec"
        "os/signal"
        "regexp"
        "strings"
        "sync"
        "syscall"
        "time"
)

var (
        procNamePat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
        envNamePat   = regexp.MustCompile("^[a-zA-z0-9_]+$")
        timeFmt      = "15:04:05"
        logPrefixFmt = "%s"
)

type Command struct {
        name string
        c    *exec.Cmd
}

func (c *Command) log(msg ...interface{}) {
        t := time.Now().Local().Format(timeFmt)
        name := fmt.Sprintf(logPrefixFmt, c.name)
        s := append([]interface{}{t, name, "|"}, msg...)
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
        pipe := func(b *bufio.Reader) {
                for {
                        line, err := b.ReadBytes('\n')
                        if err != nil {
                                break
                        }
                        c.log(strings.TrimSpace(string(line)))
                }
        }

        go pipe(bufout)
        go pipe(buferr)
        return nil
}

func commandLogInit(cmds []*Command) {
        maxlen := 0
        for _, cmd := range cmds {
                if len(cmd.name) > maxlen {
                        maxlen = len(cmd.name)
                }
        }
        logPrefixFmt = "%-" + fmt.Sprintf("%d", maxlen) + "s"
}

func LoadEnv(envfile string) ([]string, error) {
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

func LoadProcs(procfile string) (map[string]string, error) {
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

func Run(cmds []*Command) {
        if len(cmds) == 0 {
                return
        }
        commandLogInit(cmds)

        // broadcast to kill all commands' processes
        kill := make(chan bool)
        // any command finished
        done := make(chan bool, len(cmds))
        // handle Ctrl-C and other signals
        sigs := make(chan os.Signal, 1)

        signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

        var wg sync.WaitGroup
        for _, cmd := range cmds {
                if err := cmd.logging(); err != nil {
                        cmd.log("unable to redirect stderr and stdout", err)
                }
                // If you get this error, chances are the `sh' is not found
                if err := cmd.c.Start(); err != nil {
                        cmd.log(err)
                        done <- true
                        break
                }
                wg.Add(1)
                cmd.log("STARTED", "PID:", cmd.c.Process.Pid)

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
                                        cmd.log("KILLED BY SIGTERM")
                                case <-time.After(3 * time.Second):
                                        cmd.c.Process.Kill()
                                        cmd.log("KILLED BY SIGKILL")
                                }
                        case code := <-exit:
                                // `done' is a buffered channel
                                // sending msg to `done' does not block
                                if code == nil {
                                        cmd.log("EXITED: exit status 0")
                                } else {
                                        cmd.log("EXITED:", code)
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
