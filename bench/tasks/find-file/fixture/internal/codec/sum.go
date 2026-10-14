package codec

import "hash/crc32"

// ComputeChecksum returns the CRC32 of b.
func ComputeChecksum(b []byte) uint32 { return crc32.ChecksumIEEE(b) }
