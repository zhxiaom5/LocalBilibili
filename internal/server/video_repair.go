package server

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type mp4Box struct {
	typ        string
	start      int64
	size       int64
	headerSize int64
}

var faststartVideoExts = map[string]bool{
	".m4v": true,
	".mov": true,
	".mp4": true,
}

func ensureVideoSeekable(path string) (bool, error) {
	if !faststartVideoExts[strings.ToLower(filepath.Ext(path))] {
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	inspection, err := inspectMP4Faststart(file, info.Size())
	if err != nil {
		return false, err
	}
	if inspection.alreadyFaststart || !inspection.hasMoov || !inspection.hasMdat || inspection.moov.start < inspection.mdat.start {
		return false, nil
	}
	moov := inspection.moov
	if moov.size <= 0 || moov.size > info.Size() {
		return false, fmt.Errorf("invalid moov box size %d", moov.size)
	}
	moovBytes := make([]byte, moov.size)
	if _, err := file.ReadAt(moovBytes, moov.start); err != nil {
		return false, err
	}
	if err := patchChunkOffsets(moovBytes, uint64(moov.size)); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".faststart-*.tmp")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := writeFaststartMP4(tmp, file, inspection.boxes, moov, moovBytes); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	_ = os.Chtimes(tmpPath, info.ModTime(), info.ModTime())
	if err := os.Rename(tmpPath, path); err != nil {
		return false, err
	}
	cleanup = false
	return true, nil
}

type mp4Inspection struct {
	boxes            []mp4Box
	moov             mp4Box
	mdat             mp4Box
	hasMoov          bool
	hasMdat          bool
	alreadyFaststart bool
}

func inspectMP4Faststart(file *os.File, fileSize int64) (mp4Inspection, error) {
	var result mp4Inspection
	for offset := int64(0); offset < fileSize; {
		box, err := readMP4BoxHeaderAt(file, offset, fileSize)
		if err != nil {
			return mp4Inspection{}, err
		}
		if box.typ == "mdat" && result.hasMoov {
			result.mdat = box
			result.hasMdat = true
			result.alreadyFaststart = true
			return result, nil
		}
		if offset+box.size > fileSize {
			return mp4Inspection{}, fmt.Errorf("invalid MP4 box %q at %d size %d", box.typ, offset, box.size)
		}
		result.boxes = append(result.boxes, box)
		switch box.typ {
		case "moov":
			result.moov = box
			result.hasMoov = true
		case "mdat":
			if !result.hasMdat {
				result.mdat = box
				result.hasMdat = true
			}
		}
		offset += box.size
	}
	return result, nil
}

func readMP4BoxHeaderAt(file *os.File, offset int64, fileSize int64) (mp4Box, error) {
	var header [16]byte
	n, err := file.ReadAt(header[:8], offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return mp4Box{}, err
	}
	if n < 8 {
		return mp4Box{}, io.ErrUnexpectedEOF
	}
	size32 := binary.BigEndian.Uint32(header[0:4])
	box := mp4Box{typ: string(header[4:8]), start: offset, headerSize: 8}
	switch size32 {
	case 0:
		box.size = fileSize - offset
	case 1:
		n, err := file.ReadAt(header[8:16], offset+8)
		if err != nil && !errors.Is(err, io.EOF) {
			return mp4Box{}, err
		}
		if n < 8 {
			return mp4Box{}, io.ErrUnexpectedEOF
		}
		box.size = int64(binary.BigEndian.Uint64(header[8:16]))
		box.headerSize = 16
	default:
		box.size = int64(size32)
	}
	if box.size < box.headerSize {
		return mp4Box{}, fmt.Errorf("invalid MP4 box %q at %d size %d", box.typ, offset, box.size)
	}
	return box, nil
}

func writeFaststartMP4(out *os.File, in *os.File, boxes []mp4Box, moov mp4Box, patchedMoov []byte) error {
	wroteMoov := false
	for _, box := range boxes {
		if box.typ == "moov" {
			continue
		}
		if !wroteMoov && box.typ != "ftyp" {
			if _, err := out.Write(patchedMoov); err != nil {
				return err
			}
			wroteMoov = true
		}
		if _, err := in.Seek(box.start, io.SeekStart); err != nil {
			return err
		}
		if _, err := io.CopyN(out, in, box.size); err != nil {
			return err
		}
		if box.typ == "ftyp" && !wroteMoov {
			if _, err := out.Write(patchedMoov); err != nil {
				return err
			}
			wroteMoov = true
		}
	}
	if !wroteMoov {
		_, err := out.Write(patchedMoov)
		return err
	}
	return nil
}

func patchChunkOffsets(box []byte, delta uint64) error {
	return walkMP4Boxes(box, func(atom []byte, typ string) error {
		switch typ {
		case "stco":
			if len(atom) < 16 {
				return fmt.Errorf("invalid stco box")
			}
			count := int(binary.BigEndian.Uint32(atom[12:16]))
			offset := 16
			if len(atom) < offset+count*4 {
				return fmt.Errorf("invalid stco entry table")
			}
			for i := 0; i < count; i++ {
				pos := offset + i*4
				value := uint64(binary.BigEndian.Uint32(atom[pos : pos+4]))
				next := value + delta
				if next > uint64(^uint32(0)) {
					return fmt.Errorf("stco chunk offset overflow")
				}
				binary.BigEndian.PutUint32(atom[pos:pos+4], uint32(next))
			}
		case "co64":
			if len(atom) < 16 {
				return fmt.Errorf("invalid co64 box")
			}
			count := int(binary.BigEndian.Uint32(atom[12:16]))
			offset := 16
			if len(atom) < offset+count*8 {
				return fmt.Errorf("invalid co64 entry table")
			}
			for i := 0; i < count; i++ {
				pos := offset + i*8
				value := binary.BigEndian.Uint64(atom[pos : pos+8])
				binary.BigEndian.PutUint64(atom[pos:pos+8], value+delta)
			}
		}
		return nil
	})
}

func walkMP4Boxes(data []byte, visit func(atom []byte, typ string) error) error {
	for offset := 0; offset+8 <= len(data); {
		size32 := binary.BigEndian.Uint32(data[offset : offset+4])
		typ := string(data[offset+4 : offset+8])
		headerSize := 8
		var size uint64
		switch size32 {
		case 0:
			size = uint64(len(data) - offset)
		case 1:
			if offset+16 > len(data) {
				return io.ErrUnexpectedEOF
			}
			size = binary.BigEndian.Uint64(data[offset+8 : offset+16])
			headerSize = 16
		default:
			size = uint64(size32)
		}
		if size < uint64(headerSize) || uint64(offset)+size > uint64(len(data)) {
			return fmt.Errorf("invalid MP4 box %q at %d size %d", typ, offset, size)
		}
		atom := data[offset : offset+int(size)]
		if err := visit(atom, typ); err != nil {
			return err
		}
		if isMP4ContainerBox(typ) {
			childOffset := headerSize
			if typ == "meta" {
				childOffset += 4
			}
			if childOffset < len(atom) {
				if err := walkMP4Boxes(atom[childOffset:], visit); err != nil {
					return err
				}
			}
		}
		offset += int(size)
	}
	return nil
}

func isMP4ContainerBox(typ string) bool {
	switch typ {
	case "moov", "trak", "mdia", "minf", "stbl", "edts", "udta", "meta", "ilst":
		return true
	default:
		return false
	}
}
