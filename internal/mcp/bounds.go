package mcp

import (
	"errors"
	"io"
	"os"
	"syscall"
)

const (
	maxSourceBytes  = 8 << 20
	maxCatalogBytes = 2 << 20
	maxCatalogTools = 2048
	maxWireBytes    = 16 << 20
)

func readSource(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // Named discovery files are caller-selected and bounded before reading.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSourceBytes {
		return nil, errors.New("MCP discovery source is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSourceBytes {
		return nil, errors.New("MCP discovery source exceeds byte limit")
	}
	return data, nil
}
