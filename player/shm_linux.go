//go:build linux

package player

import "os"

// shmDir is where POSIX shared memory objects live: shm_open's "/name"
// is shmDir + "/name".
const shmDir = "/dev/shm"

// shmWrite creates the shared memory object name holding b.
func shmWrite(name string, b []byte) error {
	f, err := os.OpenFile(shmDir+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(shmDir + name)
	}
	return err
}

// shmExists reports the object not yet unlinked.
func shmExists(name string) bool {
	_, err := os.Lstat(shmDir + name)
	return err == nil
}

func shmRemove(name string) { _ = os.Remove(shmDir + name) }
