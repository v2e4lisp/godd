package main

import (
        "bufio"
        "fmt"
        "strings"
        "time"
)

var (
        timeFmt      = "15:04:05"
        logPrefixFmt = "%s"
        sysLogPrefix = "sys"
)

func log(name string, msg ...interface{}) {
        t := time.Now().Local().Format(timeFmt)
        name = fmt.Sprintf(logPrefixFmt, name)
        s := append([]interface{}{t, name, "|"}, msg...)
        fmt.Println(s...)
}

func syslog(msg ...interface{}) {
        log(sysLogPrefix, msg...)
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
                        log(cmd.name, strings.TrimSpace(string(line)))
                }
        }

        go pipe(bufout)
        go pipe(buferr)
        return nil
}

func logInit(cmds []*Command) {
        maxlen := 0
        for _, cmd := range cmds {
                if len(cmd.name) > maxlen {
                        maxlen = len(cmd.name)
                }
        }
        logPrefixFmt = "%-" + fmt.Sprintf("%d", maxlen) + "s"
}
