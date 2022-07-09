package main

import "net"

func main() {
	ln, err := net.Listen("tcp", "localhost:8002")
	if err != nil {
		panic(err)
	}

	for {
		con, err := ln.Accept()
		if err != nil {
			panic(err)
		}

		go func() {
			buf := make([]byte, 100)
			for {
				_, err = con.Read(buf)
				if err != nil {
					return
				}

				_, err = con.Write(buf)
				if err != nil {
					return
				}
			}
		}()

	}

}
