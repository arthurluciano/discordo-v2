package voice

// splitAnnexB cuts an H.264 access unit in Annex B (start codes 00 00 01 or 00 00 00 01) into its NAL units.
func splitAnnexB(b []byte) [][]byte {
	var nals [][]byte
	start := -1
	for i := 0; i+2 < len(b); i++ {
		if b[i] != 0 || b[i+1] != 0 || b[i+2] != 1 {
			continue
		}

		if start >= 0 {
			end := i
			if end > start && b[end-1] == 0 { // the leading zero of a 4-byte start code
				end--
			}

			if end > start {
				nals = append(nals, b[start:end])
			}
		}

		start = i + 3
		i += 2
	}

	if start >= 0 && start < len(b) {
		nals = append(nals, b[start:])
	}

	return nals
}
