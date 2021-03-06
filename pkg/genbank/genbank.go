package genbank

import (
	"bufio"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
	// "fmt"
)

// Genbank is a master struct containing all the info from a single genbank record
type Genbank struct {
	LOCUS struct {Name string; Length int; Type string; Division string; Date string} // NOT implemented
	DEFINITION string // NOT implemented
	ACCESSION string // NOT implemented
	VERSION  string // NOT implemented
	KEYWORDS string // NOT implemented
	SOURCE struct {Source string; Organism string} // NOT implemented
	REFERENCE struct {Authors string; Title string; Journal string; Pubmed string; Remark string} // NOT implemented
	COMMENT string // NOT implemented
	FEATURES []GenbankFeature // implemented
	ORIGIN []byte  // implemented
}

// genbankField is a utility struct for moving main toplevel genbank FIELDS +
// their associated lines around through channels, etc.
type genbankField struct {
	header string
	lines []string
}

// GenbankFeature is a sub-struct that contains information about one feature
// under the genbank FEATURES section
type GenbankFeature struct {
	Feature string
	Pos string
	Info map[string]string
}

// TO DO make this work on a pointer to a map instead of making copies
func updateMap(key string, value string, m map[string]string) map[string]string {
	m[key] = value
	return m
}

// true/false does this line of the file code a new FEATURE (CDS, gene, 5'UTR etc)
func isFeatureLine(line string, quoteClosed bool) bool {

	lineFields := strings.Fields(line)

	if quoteClosed {
		if len(lineFields) == 2 {
			if lineFields[0][0] != '/' {
				return true
			}
		}
	}

	return false
}

// get the FEATURES info
func parseGenbankFEATURES(field genbankField) ([]GenbankFeature) {

	rawLines := field.lines

	features := make([]GenbankFeature, 0)

	quoteClosed := true
	var feature string
	var pos string
	var gb GenbankFeature
	var keyBuffer []rune
	var valueBuffer []rune
	var isKey bool

	for linecounter, line := range(rawLines) {

		newFeature := isFeatureLine(line, quoteClosed)

		if newFeature && linecounter == 0 {

			lineFields := strings.Fields(line)

			feature = lineFields[0]
			pos = lineFields[1]

			gb = GenbankFeature{}
			gb.Feature = feature
			gb.Pos = pos
			gb.Info = make(map[string]string)

			keyBuffer = make([]rune, 0)
			valueBuffer = make([]rune, 0)

		} else if strings.TrimSpace(line)[0] == '/' && len(keyBuffer) == 0 {

			keyBuffer = make([]rune, 0)
			valueBuffer = make([]rune, 0)

			isKey = true

			quoteClosed = true

			for _, character := range(strings.TrimSpace(line)[1:]) {

				if character == '=' {
					isKey = false
					continue
				}

				if isKey == true {
					keyBuffer = append(keyBuffer, character)
				} else {
					if character == '"' {
						quoteClosed = ! quoteClosed
						continue
					}
					valueBuffer = append(valueBuffer, character)
				}
			}

		} else if ! quoteClosed {

			for _, character := range(strings.TrimSpace(line)) {
				if character == '"' {
					quoteClosed = ! quoteClosed
					continue
				}

				valueBuffer = append(valueBuffer, character)
			}

		} else if strings.TrimSpace(line)[0] == '/' && len(keyBuffer) != 0 {

			quoteClosed = true

			gb.Info = updateMap(string(keyBuffer), string(valueBuffer), gb.Info)

			keyBuffer = make([]rune, 0)
			valueBuffer = make([]rune, 0)

			isKey = true

			for _, character := range(strings.TrimSpace(line)[1:]) {

				if character == '=' {
					isKey = false
					continue
				}

				if isKey {
					keyBuffer = append(keyBuffer, character)
				} else {
					if character == '"' {
						quoteClosed = ! quoteClosed
						continue
					}
					valueBuffer = append(valueBuffer, character)
				}
			}

		} else if newFeature && linecounter != 0 {

			quoteClosed = true

			gb.Info = updateMap(string(keyBuffer), string(valueBuffer), gb.Info)
			features = append(features, gb)

			lineFields := strings.Fields(line)
			feature = lineFields[0]
			pos = lineFields[1]

			gb = GenbankFeature{}
			gb.Feature = feature
			gb.Pos = pos
			gb.Info = make(map[string]string)

			keyBuffer = make([]rune, 0)
			valueBuffer = make([]rune, 0)
		}
	}

	features = append(features, gb)

	// for _, feature := range(features){
	// 	fmt.Println(feature.feature + ", " + feature.pos)
	// 	for key, value := range(feature.info) {
	// 		fmt.Println(key + ": " + value)
	// 	}
	// 	fmt.Println(" ")
	// }

	return features
}

// get the ORIGIN info
func parseGenbankORIGIN(field genbankField) ([]byte) {

	rawLines := field.lines

	seq := make([]byte, 0)

	for _, line := range(rawLines) {
		for _, character := range(line) {
			if unicode.IsLetter(character) {
				seq = append(seq, []byte(string(character))...)
			}
		}
	}

	return seq
}

// ReadGenBank reads a genbank annotation file and returns a struct that contains
// parsed versions of the fields therein.
// Not all fields are currently parsed.
func ReadGenBank(infile string) (Genbank, error) {

	gb := Genbank{}

	f, err := os.Open(infile)
	if err != nil {
		return Genbank{}, err
	}
	defer f.Close()

	s := bufio.NewScanner(f)

	first := true
	var header string
	var lines []string
	var field genbankField

	for s.Scan() {
		line := s.Text()

		if len(line) == 0 {
			continue
		}

		r, _ := utf8.DecodeRune([]byte{line[0]})

		if unicode.IsUpper(r){
			// fmt.Println(line)
			if first {
				header = strings.Fields(line)[0]
				first = false
				continue
			}

			switch {
			case header == "FEATURES":
				field = genbankField{header: header, lines: lines}
				gb.FEATURES = parseGenbankFEATURES(field)
				// fmt.Println(gb.FEATURES)
			case header == "ORIGIN":
				field = genbankField{header: header, lines: lines}
				gb.ORIGIN = parseGenbankORIGIN(field)
				// fmt.Println(string(gb.ORIGIN))
			}

			header = strings.Fields(line)[0]
			lines = make([]string, 0)

			continue
		}

		lines = append(lines, line)
	}

	switch {
	case header == "FEATURES":
		field = genbankField{header: header, lines: lines}
		gb.FEATURES = parseGenbankFEATURES(field)
		// fmt.Println(gb.FEATURES)
	case header == "ORIGIN":
		field = genbankField{header: header, lines: lines}
		gb.ORIGIN = parseGenbankORIGIN(field)
		// fmt.Println(string(gb.ORIGIN))
	}

	// for _, feature := range(gb.FEATURES){
	// 	fmt.Println(feature.Feature + ", " + feature.Pos)
	// 	for key, value := range(feature.Info) {
	// 		fmt.Println(key + ": " + value)
	// 	}
	// 	fmt.Println(" ")
	// }
	//
	// fmt.Println(string(gb.ORIGIN))

	return gb, nil
}
