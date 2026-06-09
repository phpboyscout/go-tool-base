package openpgpkey

// z-base-32 encoder for WKD's hu/<hash> filenames.
//
// z-base-32 is Zooko's human-oriented base-32 variant (Phil Zimmermann
// alphabet, https://philzimmermann.com/docs/human-oriented-base-32-encoding.txt).
// draft-koch-openpgp-webkey-service §3.1 mandates this exact alphabet
// for WKD bucket IDs.
//
// Encoding rule: every group of 5 input bytes (40 bits) becomes 8
// output characters; trailing partial groups are padded with implicit
// zero bits — there is no '=' or other explicit padding. SHA-1 always
// produces 20 input bytes (160 bits) which encode to exactly 32
// characters, so for our specific WKD use no partial-group handling
// is needed in practice; the implementation still handles partial
// groups for completeness and ease of testing.
//
// The encoder is fewer than 50 lines, audited against the IETF spec's
// canonical test vectors in zbase32_test.go. We deliberately avoid a
// third-party dependency for this — see spec D4.

// zbase32Alphabet is the z-base-32 alphabet, indexed by 5-bit value.
const zbase32Alphabet = "ybndrfg8ejkmcpqxot1uwisza345h769"

const (
	bitsPerByte = 8
	bitsPerChar = 5    // base-32 packs 5 bits per output character
	charMask    = 0x1f // (1 << bitsPerChar) - 1
)

// zbase32Encode encodes src into a z-base-32 string. The output has
// ceil(len(src)*8/5) characters; no padding is added.
func zbase32Encode(src []byte) string {
	if len(src) == 0 {
		return ""
	}

	// Output size: ceil(bits / bitsPerChar) characters.
	outLen := (len(src)*bitsPerByte + bitsPerChar - 1) / bitsPerChar
	out := make([]byte, 0, outLen)

	var (
		buf  uint32 // bit accumulator
		bits uint   // number of bits currently in the accumulator
	)

	for _, b := range src {
		buf = (buf << bitsPerByte) | uint32(b)
		bits += bitsPerByte

		for bits >= bitsPerChar {
			bits -= bitsPerChar
			idx := (buf >> bits) & charMask
			out = append(out, zbase32Alphabet[idx])
		}
	}

	// Tail: any remaining bits become a final character, left-aligned
	// with implicit zero padding.
	if bits > 0 {
		idx := (buf << (bitsPerChar - bits)) & charMask
		out = append(out, zbase32Alphabet[idx])
	}

	return string(out)
}
