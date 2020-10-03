package sam

import (
	"errors"
	"io"
	"os"
	"unicode"
	"unicode/utf8"

	biogosam "github.com/biogo/hts/sam"
)

// getCigarOperationMap is a map of SAM CIGAR operation types to function literals
// that are used to build an aligned sequence. At the moment insertions relative
// to the reference are discarded in the query sequence - this should become optional
// in the future to handle annotation.
func getCigarOperationMap() map[string]func(int, int, int, []byte) (int, int, []byte) {
	lambda_dict := map[string]func(int, int, int, []byte) (int, int, []byte){
		"M": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			return query_start + length, ref_start + length, seq[query_start : query_start+length]
		},
		"I": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			return query_start + length, ref_start, []byte{}
		},

		"D": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			gaps := make([]byte, length)
			for i, _ := range gaps {
				gaps[i] = '-'
			}
			return query_start, ref_start + length, gaps
		},

		"N": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			gaps := make([]byte, length)
			for i, _ := range gaps {
				gaps[i] = '-'
			}
			return query_start, ref_start + length, gaps
		},

		"S": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			return query_start + length, ref_start, []byte{}
		},
		"H": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			return query_start, ref_start, []byte{}
		},
		"P": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			return query_start, ref_start, []byte{}
		},
		"=": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			return query_start + length, ref_start + length, seq[query_start : query_start+length]
		},
		"X": func(query_start, ref_start, length int, seq []byte) (int, int, []byte) {
			return query_start + length, ref_start + length, seq[query_start : query_start+length]
		}}
	return lambda_dict
}

// getOneLine processes one non-header line of a SAM file into an aligned sequence
func getOneLine(samLine biogosam.Record, refLen int) ([]byte, error) {

	lambda_dict := getCigarOperationMap()

	// QNAME := samLine.Name

	POS := samLine.Pos

	if POS < 0 {
		return []byte{}, errors.New("unmapped read")
	}

	SEQ := samLine.Seq.Expand()

	CIGAR := samLine.Cigar

	newSeqArray := make([]byte, POS)
	for i, _ := range newSeqArray {
		newSeqArray[i] = '*'
	}

	qstart := 0
	rstart := POS

	for _, op := range CIGAR {
		// fmt.Println(op.Type().String())
		// fmt.Println(op.Len())

		operation := op.Type().String()
		size := op.Len()

		new_qstart, new_rstart, extension := lambda_dict[operation](qstart, rstart, size, SEQ)

		newSeqArray = append(newSeqArray, extension...)

		qstart = new_qstart
		rstart = new_rstart

	}

	rightpad := make([]byte, refLen-len(newSeqArray))
	for i, _ := range rightpad {
		rightpad[i] = '*'
	}

	newSeqArray = append(newSeqArray, rightpad...)

	// fmt.Println(string(newSeqArray))

	return newSeqArray, nil
}

// getSetFromSlice returns the Set of bytes from an array of bytes. It is used
// to get the set of nucleotides at an alignment column within one query sequence
// in order to flatten the site properly.
func getSetFromSlice(s []byte) []byte {

	s_out := make([]byte, 0)

	m := make(map[byte]bool)

	for _, b := range s {
		m[b] = true
	}

	for key, value := range m {
		if value {
			s_out = append(s_out, key)
		}
	}

	return s_out
}

// getNucFromSite flattens a site to a single nucleotide when a query sequence
// has secondary mappings (multiple records/lines) in the SAM file.
// * If there is more than one alphabetic character at this site, an N is returned.
// * Alphabetic characters override '-'s and '*'s
func getNucFromSite(s []byte) byte {

	check := 0

	ss := getSetFromSlice(s)

	for _, e := range ss {
		r, _ := utf8.DecodeRune([]byte{e})
		if unicode.IsLetter(r) {
			check++
		}
	}

	if check > 1 {
		return 'N'
	}

	var m byte
	for i, e := range ss {
		if i == 0 || e > m {
			m = e
		}
	}

	return m
}

// checkAndGetFlattenedSeq applies getNucFromSite over all sites in a block
// of SAM records to get a single flattened sequence for one query
func checkAndGetFlattenedSeq(block [][]byte) []byte {

	seq := make([]byte, len(block[0]))
	site := make([]byte, len(block))

	for j, _ := range block[0] {
		for i, _ := range block {
			site[i] = block[i][j]
		}
		nuc := getNucFromSite(site)
		seq[j] = nuc
	}

	return seq
}

// getSeqFromBlock wraps the above functions to get a sequence from one query's
// SAM records - if there is only one line (only a primary mapping) it
// returns that aligned sequence without needing to do any flattening
func getSeqFromBlock(records []biogosam.Record, refLen int) ([]byte, error) {

	block := make([][]byte, len(records))
	for i, _ := range block {
		block[i] = make([]byte, refLen)
	}

	for i, line := range records {
		temp, err := getOneLine(line, refLen)
		if err != nil {
			return []byte{}, err
		}
		block[i] = temp
	}

	var seq []byte

	if len(block) > 1 {
		seq = checkAndGetFlattenedSeq(block)
	} else {
		seq = block[0]
	}

	// fmt.Println(seq)
	return seq, nil
}

// swapInNs replaces unmapped positions with Ns
func swapInNs(seq []byte) []byte {
	for i, L := range seq {
		if L == '*' {
			seq[i] = 'N'
		}
	}
	return seq
}

// swapInGapNss replaces internal unmapped positions with Ns and external
// unmapped positions with '-'s
func swapInGapsNs(seq []byte) []byte {
	firstLetter := true
	var firstLetterIndx int
	var lastLetterIndx int

	for i, L := range seq {
		r, _ := utf8.DecodeRune([]byte{L})
		if unicode.IsLetter(r) {

			if firstLetter {
				firstLetterIndx = i
				firstLetter = false
			}

			lastLetterIndx = i

		}
	}

	for i, L := range seq {
		if i < firstLetterIndx {
			if L == '*' {
				seq[i] = '-'
			}
		}
		if i > firstLetterIndx && i < lastLetterIndx {
			if L == '*' {
				seq[i] = 'N'
			}
		}
		if i > lastLetterIndx {
			if L == '*' {
				seq[i] = '-'
			}
		}
	}

	return seq
}

// getSamHeader uses Biogo/sam to return the header of a SAM file
func getSamHeader(infile string) (biogosam.Header, error) {
	f, err := os.Open(infile)
	if err != nil {
		return biogosam.Header{}, err
	}
	defer f.Close()

	s, err := biogosam.NewReader(f)
	if err != nil {
		return biogosam.Header{}, err
	}

	header := *s.Header()

	return header, nil
}

// groupSamRecords yields blocks of SAM records that correspond to the same query
// sequence
func groupSamRecords(infile string, chnl chan []biogosam.Record, cdone chan bool, cerr chan error) {

	f, err := os.Open(infile)
	if err != nil {
		cerr <- err
	}
	defer f.Close()

	s, err := biogosam.NewReader(f)
	if err != nil {
		cerr <- err
	}

	// fmt.Println(s.Header().Refs()[0].Name())
	// fmt.Println(s.Header().Refs()[0].Len())

	samLineGroup := make([]biogosam.Record, 0)
	first := true
	var previous string

	for {

		rec, err := s.Read()

		if err == io.EOF {

			break

		} else if err != nil {

			cerr <- err

		} else {

			if first {
				samLineGroup = append(samLineGroup, *rec)
				first = false
				previous = rec.Name
				continue
			}

			if rec.Name != previous {
				// fmt.Println(previous, len(samLineGroup))
				chnl <- samLineGroup
				samLineGroup = make([]biogosam.Record, 0)
				samLineGroup = append(samLineGroup, *rec)
				previous = rec.Name
				continue
			}

			samLineGroup = append(samLineGroup, *rec)
			previous = rec.Name

			// fmt.Println(string(rec.Seq.Expand()))
			// fmt.Println(rec.String())
		}

		// fmt.Println(previous, len(samLineGroup))
	}

	chnl <- samLineGroup

	cdone <- true
}