package app

import (
	"errors"
	"io"
	"os"
	"sync"
)

const maxAgentLogBytes int64 = 10 << 20

const truncatedLogMarker = "[older agent output truncated]\n"

type boundedLogWriter struct {
	mu      sync.Mutex
	file    *os.File
	maximum int64
}

type tailBuffer struct {
	data      []byte
	maximum   int
	truncated bool
}

func (buffer *tailBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	if buffer.maximum <= 0 {
		buffer.maximum = 32768
	}
	if len(data) >= buffer.maximum {
		buffer.data = append(buffer.data[:0], data[len(data)-buffer.maximum:]...)
		buffer.truncated = true
		return originalLength, nil
	}
	overflow := len(buffer.data) + len(data) - buffer.maximum
	if overflow > 0 {
		copy(buffer.data, buffer.data[overflow:])
		buffer.data = buffer.data[:len(buffer.data)-overflow]
		buffer.truncated = true
	}
	buffer.data = append(buffer.data, data...)
	return originalLength, nil
}

func (buffer *tailBuffer) String() string {
	if buffer.truncated {
		return truncatedLogMarker + string(buffer.data)
	}
	return string(buffer.data)
}

func openBoundedLog(path string, maximum int64) (*boundedLogWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return &boundedLogWriter{file: file, maximum: maximum}, nil
}

func (writer *boundedLogWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return 0, os.ErrClosed
	}
	originalLength := len(data)
	if writer.maximum <= 0 {
		writer.maximum = maxAgentLogBytes
	}
	if int64(len(data)) >= writer.maximum {
		data = data[len(data)-int(writer.maximum):]
		if err := writer.file.Truncate(0); err != nil {
			return 0, err
		}
		if _, err := writer.file.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		if _, err := writer.file.Write(data); err != nil {
			return 0, err
		}
		return originalLength, nil
	}
	info, err := writer.file.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size()+int64(len(data)) > writer.maximum {
		keep := writer.maximum / 2
		if keep < 1 {
			keep = 1
		}
		if info.Size() < keep {
			keep = info.Size()
		}
		tail := make([]byte, keep)
		if _, err = writer.file.ReadAt(tail, info.Size()-keep); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if err = writer.file.Truncate(0); err != nil {
			return 0, err
		}
		if _, err = writer.file.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		marker := truncatedLogMarker
		if int64(len(marker)) > writer.maximum-keep {
			marker = ""
		}
		if _, err = writer.file.Write([]byte(marker)); err != nil {
			return 0, err
		}
		if _, err = writer.file.Write(tail); err != nil {
			return 0, err
		}
	}
	remaining := writer.maximum
	if info, statErr := writer.file.Stat(); statErr == nil {
		remaining -= info.Size()
	}
	if remaining <= 0 {
		return originalLength, nil
	}
	if int64(len(data)) > remaining {
		data = data[len(data)-int(remaining):]
	}
	if _, err = writer.file.Seek(0, io.SeekEnd); err != nil {
		return 0, err
	}
	if _, err = writer.file.Write(data); err != nil {
		return 0, err
	}
	return originalLength, nil
}

func (writer *boundedLogWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

func readLogTail(path string, maximum int64) ([]byte, error) {
	if maximum <= 0 {
		return []byte{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := info.Size() - maximum
	if start < 0 {
		start = 0
	}
	if _, err = file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(file, maximum))
}
