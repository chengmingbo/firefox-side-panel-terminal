//go:build !unix

package main

func newUnixBackend() ptyBackend { return nil }
