package stream

import "encoding/binary"

const (
	dataHeaderSizeV1 = 1 + 1 + 1 + 16 + 8 + 8
	ackHeaderSizeV1  = 1 + 1 + 1 + 16 + 8 + 4 + 4
)

func decodeDataHeaderV1(payload []byte) (hdr streamDataHeaderV1, body []byte, ok bool) {
	if len(payload) < dataHeaderSizeV1 || payload[0] != kindData {
		return streamDataHeaderV1{}, nil, false
	}
	hdr.Ver = payload[1]
	hdr.Flags = payload[2]
	copy(hdr.DeliveryID[:], payload[3:19])
	hdr.Position = binary.BigEndian.Uint64(payload[19:27])
	hdr.PTSMs = binary.BigEndian.Uint64(payload[27:35])
	return hdr, payload[35:], true
}

func encodeDataHeaderV1(deliveryID [16]byte, position, ptsMs uint64, flags uint8, body []byte) []byte {
	out := make([]byte, dataHeaderSizeV1+len(body))
	out[0] = kindData
	out[1] = headerVersionV1
	out[2] = flags
	copy(out[3:19], deliveryID[:])
	binary.BigEndian.PutUint64(out[19:27], position)
	binary.BigEndian.PutUint64(out[27:35], ptsMs)
	copy(out[35:], body)
	return out
}

func decodeAckHeaderV1(payload []byte) (hdr streamAckHeaderV1, ok bool) {
	if len(payload) < ackHeaderSizeV1 || payload[0] != kindAck {
		return streamAckHeaderV1{}, false
	}
	hdr.Ver = payload[1]
	hdr.Flags = payload[2]
	copy(hdr.DeliveryID[:], payload[3:19])
	hdr.Position = binary.BigEndian.Uint64(payload[19:27])
	hdr.CreditUnits = binary.BigEndian.Uint32(payload[27:31])
	hdr.Reserved = binary.BigEndian.Uint32(payload[31:35])
	return hdr, true
}

func encodeAckHeaderV1(deliveryID [16]byte, position uint64, creditUnits uint32, flags uint8) []byte {
	out := make([]byte, ackHeaderSizeV1)
	out[0] = kindAck
	out[1] = headerVersionV1
	out[2] = flags
	copy(out[3:19], deliveryID[:])
	binary.BigEndian.PutUint64(out[19:27], position)
	binary.BigEndian.PutUint32(out[27:31], creditUnits)
	return out
}
