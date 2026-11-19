//go:build !linux

package player

import "errors"

func shmWrite(string, []byte) error { return errors.ErrUnsupported }
func shmExists(string) bool         { return false }
func shmRemove(string)              {}
