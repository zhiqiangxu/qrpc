package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	con, err := net.Dial("tcp", "localhost:8002")
	if err != nil {
		panic(err)
	}

	sbuf := make([]byte, 100)
	rbuf := make([]byte, 100)
	sbuf[0] = 'a'
	for {
		start := time.Now()
		con.Write(sbuf)

		_, err = con.Read(rbuf)

		fmt.Println("cost", time.Since(start), string(rbuf[0]))
		time.Sleep(time.Second)
		sbuf[0] += 1

	}
}
