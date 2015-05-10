package main

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	waitKill = 3 * time.Second
)

type Command struct {
	name string
	c    *exec.Cmd
}

func Run(cmds []*Command) {
	if len(cmds) == 0 {
		return
	}
	logInit(cmds)

	// broadcast to kill all commands' processes
	kill := make(chan bool)
	// any command finished
	done := make(chan bool, len(cmds))
	// handle Ctrl-C and other signals
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	var wg sync.WaitGroup
	for _, cmd := range cmds {
		if err := logging(cmd); err != nil {
			log(cmd.name, "unable to redirect stderr and stdout", err)
		}
		// If you get this error, chances are the `sh' is not found
		if err := cmd.c.Start(); err != nil {
			log(cmd.name, err)
			done <- true
			break
		}
		wg.Add(1)
		log(cmd.name, "[STARTED] pid:", cmd.c.Process.Pid)

		exit := make(chan error)
		go func(cmd *Command, exit chan error) {
			exit <- cmd.c.Wait()
		}(cmd, exit)

		// To prevent killing a terminated command,
		// send a message to the `done' channel and exit the goroutine
		go func(cmd *Command, exit chan error) {
			defer wg.Done()
			defer func() { done <- true }()
			defer func() { log(cmd.name, "[EXITED]", cmd.c.ProcessState) }()

			select {
			case <-exit:
			case <-kill:
				syslog("sending SIGTERM to", cmd.name)
				cmd.c.Process.Signal(syscall.SIGTERM)
				// if SIGTERM cannot kill the process,
				// send it a SIGKILL
				select {
				case <-exit:
				case <-time.After(waitKill):
					syslog("sending SIGKILL to", cmd.name)
					cmd.c.Process.Kill()
				}
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
