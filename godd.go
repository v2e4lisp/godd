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

func log(c *Command, msg ...interface{}) {
        t := time.Now().Local().Format(timeFmt)
        name := fmt.Sprintf(logPrefixFmt, c.name)
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
