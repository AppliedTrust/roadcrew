package main

import (
	"log"
	"os/exec"
	"syscall"
)

func main() {
	err := syscall.Setuid(0)
	if err != nil {
		log.Fatal(err)
	}
	cmd := exec.Command("/usr/local/sbin/restart_icinga")
	output, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	log.Println(string(output))
}
