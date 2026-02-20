package file

import (
	"os"
	"sync"
	"time"
)

type recvSession struct {
	id       [16]byte
	provider uint32
	consumer uint32

	dir       string
	name      string
	finalPath string
	partPath  string

	size      uint64
	sha256Hex string
	overwrite bool

	mu              sync.Mutex
	file            *os.File
	expectedOffset  uint64
	pending         map[uint64][]byte
	pendingBytes    uint64
	maxPendingBytes uint64
	finSeen         bool
	lastActive      time.Time
	lastAckOffset   uint64
	lastAckTime     time.Time
}

type sendSession struct {
	id       [16]byte
	provider uint32
	consumer uint32

	dir      string
	name     string
	filePath string

	size      uint64
	sha256Hex string
	startFrom uint64

	mu         sync.Mutex
	ackedUntil uint64
	lastActive time.Time
}
