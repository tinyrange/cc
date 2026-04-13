package initramfs

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

type File struct {
	Path     string
	Mode     os.FileMode
	Data     []byte
	Type     Type
	DevMajor uint32
	DevMinor uint32
}

type Type int

const (
	TypeRegular Type = iota
	TypeDirectory
	TypeCharDevice
)

const (
	newcMagic           = "070701"
	newcHeaderLen       = 110
	newcTrailerName     = "TRAILER!!!"
	newcRegularFileBit  = 0o100000
	newcDirectoryBit    = 0o040000
	newcCharDeviceBit   = 0o020000
)

func Build(files []File) ([]byte, error) {
	buf := &bytes.Buffer{}
	ino := uint32(1)

	for idx, file := range files {
		name := strings.TrimPrefix(file.Path, "/")
		if name == "" {
			return nil, fmt.Errorf("file %d has empty path", idx)
		}
		mode, err := encodeMode(file)
		if err != nil {
			return nil, fmt.Errorf("file %d (%s): %w", idx, file.Path, err)
		}
		if err := writeEntry(buf, entry{
			ino:      ino,
			mode:     mode,
			nlink:    1,
			filesize: uint32(len(file.Data)),
			name:     name,
			data:     file.Data,
			rdevmajor: file.DevMajor,
			rdevminor: file.DevMinor,
		}); err != nil {
			return nil, fmt.Errorf("write file %d (%s): %w", idx, file.Path, err)
		}
		ino++
	}

	if err := writeEntry(buf, entry{
		mode:  newcRegularFileBit,
		nlink: 1,
		name:  newcTrailerName,
	}); err != nil {
		return nil, fmt.Errorf("write trailer: %w", err)
	}

	return buf.Bytes(), nil
}

type entry struct {
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

func writeEntry(buf *bytes.Buffer, e entry) error {
	nameSize := len(e.name) + 1
	header := fmt.Sprintf("%s%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x",
		newcMagic,
		e.ino,
		e.mode,
		e.uid,
		e.gid,
		e.nlink,
		e.mtime,
		e.filesize,
		e.devmajor,
		e.devminor,
		e.rdevmajor,
		e.rdevminor,
		nameSize,
		0,
	)
	if len(header) != newcHeaderLen {
		return fmt.Errorf("unexpected header length %d", len(header))
	}
	if _, err := buf.WriteString(header); err != nil {
		return err
	}
	if _, err := buf.WriteString(e.name); err != nil {
		return err
	}
	if err := buf.WriteByte(0); err != nil {
		return err
	}
	if pad := alignTo4(newcHeaderLen + nameSize); pad > 0 {
		if _, err := buf.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	if len(e.data) > 0 {
		if _, err := buf.Write(e.data); err != nil {
			return err
		}
	}
	if pad := alignTo4(len(e.data)); pad > 0 {
		if _, err := buf.Write(make([]byte, pad)); err != nil {
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

func encodeMode(file File) (uint32, error) {
	mode := uint32(file.Mode.Perm())
	switch file.Type {
	case TypeRegular:
		return mode | newcRegularFileBit, nil
	case TypeDirectory:
		return mode | newcDirectoryBit, nil
	case TypeCharDevice:
		return mode | newcCharDeviceBit, nil
	default:
		return 0, fmt.Errorf("unsupported file type %d", file.Type)
	}
}
