package file

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func newUUID() ([16]byte, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return [16]byte{}, err
	}
	// UUIDv4
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}

func uuidToString(id [16]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], id[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], id[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], id[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], id[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], id[10:16])
	return string(buf[:])
}

func parseUUID(s string) ([16]byte, bool) {
	var id [16]byte
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) != 36 {
		return [16]byte{}, false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return [16]byte{}, false
	}
	raw := strings.ReplaceAll(s, "-", "")
	if len(raw) != 32 {
		return [16]byte{}, false
	}
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 16 {
		return [16]byte{}, false
	}
	copy(id[:], b)
	return id, true
}
