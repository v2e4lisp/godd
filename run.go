package main

import (
        "log"
        "os"
        "os/exec"
        "os/signal"
        "sync"
        "syscall"
        "time"

        "github.com/v2e4lisp/cmdplx"
)

type runner struct {
        cmds     map[*exec.Cmd]string
        waitKill time.Duration
        once     sync.Once
        done     chan struct{}
}

func (r *runner) killAll() {
        go r.once.Do(func() {
                for cmd, name := range r.cmds {
                        if cmd.Process != nil {
                                log.Println("sys", "sending SIGTERM to", name)
                                cmd.Process.Signal(syscall.SIGTERM)
                        }
                }

                select {
                case <-time.After(r.waitKill):
                        for cmd, name := range r.cmds {
                                if cmd.Process != nil {
                                        log.Println("sys", "sending SIGKILL to", name)
                                        cmd.Process.Kill()
                                }
                        }
                case <-r.done:
                }
        })
}

func (r *runner) handleSigs() {
        // handle Ctrl-C and other signals
        sigs := make(chan os.Signal, 1)
        signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
        go func() {
                for {
                        select {
                        case <-sigs:
                                r.killAll()
                        case <-r.done:
                                return
                        }
                }
        }()
}

func (r *runner) run() {
        r.handleSigs()
        cs := []*exec.Cmd(nil)
        for c := range r.cmds {
                cs = append(cs, c)
        }
        plx := cmdplx.New(cs)
        lines, started, exited := plx.Start()

        for {
                select {
                case line := <-lines:
                        if line == nil {
                                lines = nil
                                break
                        }
                        if line.Err() == nil {
                                log.Println(r.cmds[line.Cmd()], line.Text())
                        }
                case s := <-started:
                        if s == nil {
                                started = nil
                                break
                        }
                        c := s.Cmd()
                        if err := s.Err(); err != nil {
                                log.Println(r.cmds[c], err)
                                r.killAll()
                                break
                        }
                        log.Println(r.cmds[c], "[Started]", "pid:", c.Process.Pid)
                case s := <-exited:
                        if s == nil {
                                goto DONE
                        }
                        c := s.Cmd()
                        log.Println(r.cmds[c], "[Exited]", c.ProcessState)
                        r.killAll()
                }
        }

DONE:
        close(r.done)
}

func Run(cmds map[*exec.Cmd]string, waitKill time.Duration) {
        r := &runner{
                cmds:     cmds,
                waitKill: waitKill,
                done:     make(chan struct{}),
        }
        r.run()
}
