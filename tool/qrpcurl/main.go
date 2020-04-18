package main

import (
	"fmt"
	"log"
	"os"

	"strings"

	"strconv"

	"github.com/urfave/cli/v2"
	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/ws/client"
)

func main() {
	app := &cli.App{
		Usage:     "It's basically curl for qRPC servers",
		UsageText: "qrpcurl [-f flag] [-w] host:port/cmd payload",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "flag", Aliases: []string{"f"}},
			&cli.BoolFlag{Name: "web", Aliases: []string{"w"}},
		},
		Action: func(c *cli.Context) (err error) {
			if c.NArg() != 2 {
				cli.ShowAppHelp(c)
				return
			}

			url := c.Args().Get(0)
			payload := c.Args().Get(1)

			parts := strings.Split(url, "/")
			if len(parts) == 1 {
				fmt.Println("cmd not specified!")
				return
			} else if len(parts) > 2 {
				cli.ShowAppHelp(c)
				return
			}

			hostAndPort := parts[0]
			cmdStr := parts[1]

			cmdInt, err := strconv.Atoi(cmdStr)
			if err != nil {
				fmt.Println("cmd must be integer, got", cmdStr)
				return
			}

			if cmdInt < 0 || cmdInt > qrpc.MaxCmd {
				fmt.Println("cmd must be in the range [0,", qrpc.MaxCmd, "]")
				return
			}

			if len(strings.Split(hostAndPort, ":")) != 2 {
				fmt.Println("port not specified!")
				return
			}

			var (
				conn *qrpc.Connection
			)

			if c.Bool("web") {
				conn, err = client.NewConnection(hostAndPort, qrpc.ConnectionConfig{}, nil)
			} else {
				conn, err = qrpc.NewConnection(hostAndPort, qrpc.ConnectionConfig{}, nil)
			}
			if err != nil {
				fmt.Println("connect fail:", err)
				return
			}

			flag := c.Int("flag")
			if flag < 0 || flag > 0xff {
				fmt.Println("flag must be in the range [0, 255]")
				return
			}

			_, resp, err := conn.Request(qrpc.Cmd(cmdInt), qrpc.FrameFlag(flag), []byte(payload))
			if err != nil {
				fmt.Println("request fail:", err)
				return
			}

			fmt.Println("[request]")
			fmt.Println("flag:", flag)
			fmt.Println("cmd:", cmdInt)
			fmt.Println("payload:")
			fmt.Println(payload)
			fmt.Println("")

			frame, err := resp.GetFrame()
			if err != nil {
				fmt.Println("get response fail:", err)
				return
			}

			fmt.Println("[response]")
			fmt.Println("flag:", frame.Flags)
			fmt.Println("cmd:", frame.Cmd)
			fmt.Println("payload:")
			fmt.Println(string(frame.Payload))

			return
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
