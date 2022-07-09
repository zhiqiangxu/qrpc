package main

import "net"

func main() {
	ln, err := net.Listen("tcp", "localhost:8002")
	if err != nil {
		panic(err)
	}

	con, err := ln.Accept()
	if err != nil {
		panic(err)
	}

	buf := make([]byte, 100)

	for {
		_, err = con.Read(buf)

		con.Write(buf)
	}

}
