package sam

import (
	"io"
	"os"
	"sort"
	"sync"
	"errors"
	"strconv"
	"strings"
	"runtime"
	biogosam "github.com/biogo/hts/sam"
)

// TODO: tidy this up wrt to the struct(s) in topa.go
type insOccurrence struct {
	query string
	start int
	seq string
}

type delOccurrence struct {
	query string
	start int
	length int
}

func getSamRecords(infile string, chnl chan biogosam.Record, cdone chan bool, cerr chan error) {

	var err error

	f, err := os.Open(infile)
	if err != nil {
		cerr<- err
	}

	defer f.Close()

	s, err := biogosam.NewReader(f)
	if err != nil {
		cerr<- err
	}

	for {
		rec, err := s.Read()

		if err == io.EOF {

			break

		} else if err != nil {

			cerr<- err

		} else {
			// if this read is unmapped, then skip it.
			// the third bit (== 4) in the sam flag is set if the read is unmapped,
			// can use the rightshift method to check this:
			if ((rec.Flags >> 2) & 1) == 1 {
				os.Stderr.WriteString("skipping unmapped read: " + rec.Name + "\n")
				continue
			}

			chnl<- *rec

		}
	}

	cdone <- true
}

func getIndels(cSR chan biogosam.Record, cIns chan insOccurrence, cDel chan delOccurrence, cErr chan error) {

	lambda_dict := getCigarOperationMapNoInsertions()

	var ins insOccurrence
	var del delOccurrence

	for samLine := range(cSR) {

		QNAME := samLine.Name

		POS := samLine.Pos

		if POS < 0 {
			cErr<- errors.New("unmapped read")
		}

		SEQ := samLine.Seq.Expand()

		CIGAR := samLine.Cigar

		qstart := 0
		rstart := POS

		for _, op := range CIGAR {

			operation := op.Type().String()
			size := op.Len()

			if operation == "I" {
				ins = insOccurrence{query: QNAME, start: rstart, seq: string(SEQ[qstart:qstart + size])}
				cIns<- ins
			}

			if operation == "D" {
				del = delOccurrence{query: QNAME, start: rstart, length: size}
				cDel<- del
			}

			new_qstart, new_rstart, _ := lambda_dict[operation](qstart, rstart, size, SEQ)

			qstart = new_qstart
			rstart = new_rstart

		}
	}

	return
}

func populateInsMap(cIns chan insOccurrence, cInsMap chan map[int]map[string][]string, cErr chan error)  {

	insMap := make(map[int]map[string][]string)

	// type insertionOccurrence struct {
	// 	query string
	// 	start int
	// 	seq string
	// }

	var q string
	var strt int
	var sq string

	for ins := range(cIns) {
		q = ins.query
		strt = ins.start
		sq = ins.seq

		if _, ok := insMap[strt]; ok {
			if _, ok := insMap[strt][sq]; ok {
				insMap[strt][sq] = append(insMap[strt][sq], q)
			} else {
				insMap[strt][sq] = []string{q}
			}
		} else {
			insMap[strt] = make(map[string][]string)
			insMap[strt][sq] = []string{q}
		}
	}

	cInsMap<- insMap
}

func populateDelMap(cDel chan delOccurrence, cDelMap chan map[int]map[int][]string, cErr chan error)  {

	delMap := make(map[int]map[int][]string)

	// type deletionOccurrence struct {
	// 	query string
	// 	start int
	// 	length int
	// }

	var q string
	var strt int
	var ln int

	for del := range(cDel) {
		q = del.query
		strt = del.start
		ln = del.length

		if _, ok := delMap[strt]; ok {
			if _, ok := delMap[strt][ln]; ok {
				delMap[strt][ln] = append(delMap[strt][ln], q)
			} else {
				delMap[strt][ln] = []string{q}
			}
		} else {
			delMap[strt] = make(map[int][]string)
			delMap[strt][ln] = []string{q}
		}
	}

	cDelMap<- delMap
}

func writeInsMap(outfile string, insmap map[int]map[string][]string, threshold int) error {

	keys := make([]int, 0, len(insmap))
	for k := range insmap {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	f, err := os.Create(outfile)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.WriteString("ref_start\tinsertion\tsamples\n")
	if err != nil {
		return err
	}

	for _, k := range(keys) {
		for v := range(insmap[k]) {
			if len(insmap[k][v]) < threshold {
				continue
			}
			// k + 1 to get things in 1-based coordinates
			c1 := strconv.Itoa(k + 1)
			c2 := v
			c3 := strings.Join(insmap[k][v], "|")

			_, err = f.WriteString(c1 + "\t" + c2 + "\t" + c3 + "\n")
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func writeDelMap(outfile string, delmap map[int]map[int][]string, threshold int) error {

	keys := make([]int, 0, len(delmap))
	for k := range delmap {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	f, err := os.Create(outfile)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.WriteString("ref_start\tlength\tsamples\n")
	if err != nil {
		return err
	}

	for _, k := range(keys) {
		for v := range(delmap[k]) {
			if len(delmap[k][v]) < threshold {
				continue
			}
			// k + 1 to get things in 1-based coordinates
			c1 := strconv.Itoa(k + 1)
			c2 := strconv.Itoa(v)
			c3 := strings.Join(delmap[k][v], "|")

			_, err = f.WriteString(c1 + "\t" + c2 + "\t" + c3 + "\n")
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func Indels(samFile string, insOut string, delOut string, threshold int) error {
	cErr := make(chan error)

	cSR := make(chan biogosam.Record, runtime.NumCPU())

	cIns := make(chan insOccurrence)
	cDel := make(chan delOccurrence)

	cInsMap := make(chan map[int]map[string][]string)
	cDelMap := make(chan map[int]map[int][]string)

	cReadDone := make(chan bool)
	cInDelsDone := make(chan bool)

	go getSamRecords(samFile, cSR, cReadDone, cErr)

	var wgInDels sync.WaitGroup
	wgInDels.Add(runtime.NumCPU())

	for n := 0; n < runtime.NumCPU(); n++ {
		go func() {
			getIndels(cSR, cIns, cDel, cErr)
			wgInDels.Done()
		}()
	}

	go populateInsMap(cIns, cInsMap, cErr)
	go populateDelMap(cDel, cDelMap, cErr)

	go func() {
		wgInDels.Wait()
		cInDelsDone<- true
	}()

	for n := 1; n > 0; {
		select {
		case err := <-cErr:
			return err
		case <-cReadDone:
			close(cSR)
			n--
		}
	}

	for n := 1; n > 0; {
		select {
		case err := <-cErr:
			return err
		case <-cInDelsDone:
			close(cIns)
			close(cDel)
			n--
		}
	}

	var insertionmap map[int]map[string][]string
	var deletionmap map[int]map[int][]string

	for n := 2; n > 0; {
		select {
		case err := <-cErr:
			return err
		case insertionmap = <-cInsMap:
			// close(cInsMap)
			n--
		case deletionmap = <-cDelMap:
			// close(cDelMap)
			n--
		}
	}

	err := writeInsMap(insOut, insertionmap, threshold)
	if err != nil {
		return err
	}

	err = writeDelMap(delOut, deletionmap, threshold)
	if err != nil {
		return err
	}

	return nil
}
