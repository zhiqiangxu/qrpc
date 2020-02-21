package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	cmd       = flag.Int("c", 0, "cmd for qrpc")
	flg       = flag.Int("f", 0, "flag for qrpc")
	requestID = flag.Uint64("r", 0, "request id for qrpc")
	payload   = flag.String("p", "", "payload for qrpc")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if *cmd < 0 || *cmd > 0xffffff {
		log.Fatal("invalid cmd")
	}

	if *flg < 0 || *flg > 0xff {
		log.Fatal("invalid cmd")
	}

	buf := make([]byte, 16+len(*payload))

	length := len(buf) - 4
	binary.BigEndian.PutUint32(buf, uint32(length))
	binary.BigEndian.PutUint64(buf[4:], *requestID)
	cmdAndFlags := uint32(*flg)<<24 + uint32(*cmd)&0xffffff
	binary.BigEndian.PutUint32(buf[12:], cmdAndFlags)
	copy(buf[16:], []byte(*payload))

	fmt.Printf("packet is: %x\nlength:%x\nrequestID:%x\nflag:%x\ncmd:%x\npayload:%x\n", buf, buf[0:4], buf[4:12], buf[12:13], buf[13:16], buf[16:])
}
