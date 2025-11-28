package boot

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

type initramfsFile struct {
	Path     string
	Mode     os.FileMode
	Data     []byte
	DevMajor int
	DevMinor int
}

const (
	newcMagic          = "070701"
	newcHeaderLen      = 110
	newcTrailerName    = "TRAILER!!!"
	newcRegularFileBit = 0o100000
)

func buildInitramfs(files []initramfsFile) ([]byte, error) {
	buf := &bytes.Buffer{}
	ino := uint32(1)

	for idx, file := range files {
		name := strings.TrimPrefix(file.Path, "/")
		if name == "" {
			return nil, fmt.Errorf("initramfs file %d has empty name", idx)
		}
		mode := uint32(file.Mode.Perm()) | newcRegularFileBit
		if err := writeNewcEntry(buf, newcEntry{
			ino:      ino,
			mode:     mode,
			nlink:    1,
			filesize: uint32(len(file.Data)),
			name:     name,
			data:     file.Data,
			devmajor: uint32(file.DevMajor),
			devminor: uint32(file.DevMinor),
		}); err != nil {
			return nil, fmt.Errorf("write initramfs file %d (%s): %w", idx, file.Path, err)
		}
		ino++
	}

	if err := writeNewcEntry(buf, newcEntry{
		ino:      0,
		mode:     newcRegularFileBit,
		nlink:    1,
		filesize: 0,
		name:     newcTrailerName,
		data:     nil,
	}); err != nil {
		return nil, fmt.Errorf("write cpio trailer: %w", err)
	}

	return buf.Bytes(), nil
}

type newcEntry struct {
	ino       uint32
	mode      uint32
	uid       uint32
	gid       uint32
	nlink     uint32
	mtime     uint32
	filesize  uint32
	devmajor  uint32
	devminor  uint32
	rdevmajor uint32
	rdevminor uint32
	name      string
	data      []byte
}

func writeNewcEntry(buf *bytes.Buffer, entry newcEntry) error {
	nameSize := len(entry.name) + 1 // include trailing NUL
	header := fmt.Sprintf("%s%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x",
		newcMagic,
		entry.ino,
		entry.mode,
		entry.uid,
		entry.gid,
		entry.nlink,
		entry.mtime,
		entry.filesize,
		entry.devmajor,
		entry.devminor,
		entry.rdevmajor,
		entry.rdevminor,
		nameSize,
		0,
	)
	if len(header) != newcHeaderLen {
		return fmt.Errorf("unexpected header length %d", len(header))
	}
	if _, err := buf.WriteString(header); err != nil {
		return err
	}
	if _, err := buf.WriteString(entry.name); err != nil {
		return err
	}
	if err := buf.WriteByte(0); err != nil {
		return err
	}

	pad := alignTo4(newcHeaderLen + nameSize)
	if pad > 0 {
		if _, err := buf.Write(make([]byte, pad)); err != nil {
			return err
		}
	}

	if len(entry.data) > 0 {
		if _, err := buf.Write(entry.data); err != nil {
			return err
		}
	}
	dataPad := alignTo4(len(entry.data))
	if dataPad > 0 {
		if _, err := buf.Write(make([]byte, dataPad)); err != nil {
			return err
		}
	}

	return nil
}

func alignTo4(length int) int {
	if length%4 == 0 {
		return 0
	}
	return 4 - (length % 4)
}
