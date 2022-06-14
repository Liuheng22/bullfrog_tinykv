package raft

import "log"

// Debugging
const Debug = false
const flag = "Debug2B"

var ToB = false
var ToC = false
var To3A = true

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

func ToBPrint(format string, a ...interface{}) {
	if flag == "Debug2B" && ToB == true {
		log.Printf(format, a...)
	}
}

func ToCPrint(format string, a ...interface{}) {
	if ToC == true {
		log.Printf(format, a...)
	}
}

func To3APrint(format string, a ...interface{}) {
	if To3A == true {
		log.Printf(format, a...)
	}
}
