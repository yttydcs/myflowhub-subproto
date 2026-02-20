package file

import "encoding/binary"

const binHeaderV1Size = 1 + 1 + 2 + 16 + 8

const (
	binVerV1   = 1
	binFlagFIN = 1 << 0
)

type binHeaderV1 struct {
	Ver       uint8
	Flags     uint8
	Reserved  uint16
	SessionID [16]byte
	Offset    uint64
}

func decodeBinHeaderV1(payload []byte) (kind byte, hdr binHeaderV1, body []byte, ok bool) {
	if len(payload) < 1 {
		return 0, binHeaderV1{}, nil, false
	}
	kind = payload[0]
	if kind != kindData && kind != kindAck {
		return kind, binHeaderV1{}, nil, false
	}
	if len(payload) < 1+binHeaderV1Size {
		return kind, binHeaderV1{}, nil, false
	}
	i := 1
	hdr.Ver = payload[i]
	hdr.Flags = payload[i+1]
	hdr.Reserved = binary.BigEndian.Uint16(payload[i+2 : i+4])
	copy(hdr.SessionID[:], payload[i+4:i+20])
	hdr.Offset = binary.BigEndian.Uint64(payload[i+20 : i+28])
	body = payload[i+28:]
	return kind, hdr, body, true
}

func encodeBinHeaderV1(kind byte, sessionID [16]byte, offset uint64, fin bool, body []byte) []byte {
	if kind != kindData && kind != kindAck {
		return nil
	}
	outLen := 1 + binHeaderV1Size
	if kind == kindData {
		outLen += len(body)
	}
	out := make([]byte, outLen)
	out[0] = kind
	out[1] = binVerV1
	flags := uint8(0)
	if fin {
		flags |= binFlagFIN
	}
	out[2] = flags
	// reserved [3:5] keep 0
	copy(out[5:21], sessionID[:])
	binary.BigEndian.PutUint64(out[21:29], offset)
	if kind == kindData && len(body) > 0 {
		copy(out[29:], body)
	}
	return out
}
