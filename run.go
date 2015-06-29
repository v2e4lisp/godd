package main

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
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

func logging(cmd *Command) error {
	stdout, err := cmd.c.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.c.StderrPipe()
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
			log.Println(cmd.name, strings.TrimSpace(string(line)))
		}
	}

	go pipe(bufout)
	go pipe(buferr)
	return nil
}

func Run(cmds []*Command) {
	if len(cmds) == 0 {
		return
	}

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
			log.Println(cmd.name, "unable to redirect stderr and stdout", err)
		}
		// If you get this error, chances are the `sh' is not found
		if err := cmd.c.Start(); err != nil {
			log.Println(cmd.name, err)
			done <- true
			break
		}
		log.Println(cmd.name, "[STARTED] pid:", cmd.c.Process.Pid)

		exit := make(chan error)
		go func(cmd *Command, exit chan error) { exit <- cmd.c.Wait() }(cmd, exit)
		// To prevent killing a terminated command,
		// send a message to the `done' channel and exit the goroutine
		go func(cmd *Command, exit chan error) {
			wg.Add(1)
			defer wg.Done()
			defer func() { done <- true }()
			defer func() { log.Println(cmd.name, "[EXITED]", cmd.c.ProcessState) }()

			select {
			case <-exit:
			case <-kill:
				log.Println("sys", "sending SIGTERM to", cmd.name)
				cmd.c.Process.Signal(syscall.SIGTERM)
				// if SIGTERM cannot kill the process,
				// send it a SIGKILL
				select {
				case <-exit:
				case <-time.After(waitKill):
					log.Println("sys", "sending SIGKILL to", cmd.name)
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
