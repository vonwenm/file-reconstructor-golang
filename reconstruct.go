// App file_reconstructor encodes to or decodes from a redundant format which
// is resistant to bit errors.

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
)

// This is a compile-time option because we don't want to have to trust users
// to set it correctly, or to have to worry about it changing from stream to
// stream. 10000 duplications consumes only 80kb but makes it quite likely we
// can recover the length even with random bit error rates as high as 48%.
const LengthRedundancy = 10000

// Mostly here for debugging. Usually you want to keep writing until the block
// device is full, regardless of the number of copies that will be.
//const MaxNumCopies = 10
const MaxNumCopies = math.MaxUint64

// Seeking on the input can be disabled. When seeking is disabled, all data is
// buffered into ram.
const (
	//useSeek = true
	useSeek = false
)

var verbose = true
var verbose2 = true

var endian = binary.BigEndian

func main() {
	if isDecode {
		decode()
	} else {
		encode()
	}
}

const (
	WordSize  = 64
	WordBytes = 8
)

type WordValues struct {
	OneCount [WordSize]uint64

	// the zero count for each byte is Count - OneCount[i]
	Count uint64
}

func (w *WordValues) AddWord(word uint64) {
	var i uint64

	for i = 0; i < WordSize; i++ {
		var mask uint64

		mask = 1 << i
		w.OneCount[i] += (word & mask) >> i
	}

	w.Count += 1
}

// Outputs confidence data about EACH BIT. Truly, a ton of output.
const debugDecodeWord = false

var totalBitErrors uint64

func (w *WordValues) DecodeWord() (word uint64) {
	threshold := w.Count / 2

	for i := uint64(0); i < WordSize; i++ {
		oc := w.OneCount[i]

		if debugDecodeWord { /// {{{
			diff := int64(oc) - int64(threshold)
			if diff < 0 {
				diff = int64(threshold) - int64(oc)
			}
			var val int
			if oc > threshold {
				val = 1
			}
			fmt.Printf("bit %d: %d, confidence %0.1f%% (%d/%d)\n", i, val, float64(diff)/float64(threshold)*100, diff, threshold)
		} // }}}

		if oc > threshold {
			word |= 1 << i
			totalBitErrors += w.Count - oc
		} else {
			totalBitErrors += oc
		}
	}

	return
}

func decode() {
	w := WordValues{}

	for i := 0; i < LengthRedundancy; i++ {
		var v uint64
		binary.Read(os.Stdin, endian, &v)
		w.AddWord(v)
	}

	fmt.Fprintf(os.Stderr, "%+v\n", w)

	dataLength := w.DecodeWord()

	fmt.Fprintln(os.Stderr, "dataLength:", dataLength)

	_, err := os.Stdin.Seek(0, 1)
	if err == nil && useSeek {
		decodeReadSeeker(os.Stdin, dataLength)
	} else {
		const seekMsg = " Attempting to buffer entire input (may consume lots of RAM!)"
		if useSeek {
			log.Printf("Stdin not seekable!" + seekMsg)
		} else {
			log.Printf("Seeking disabled." + seekMsg)
		}
		data, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		buf := bytes.NewReader(data)
		log.Printf("Input buffered successfully. Proceeding...")
		decodeReadSeeker(buf, dataLength)
	}

	fmt.Fprintln(os.Stderr, "Total bits in error detected on media:", totalBitErrors, "(there may be more)")
}

func paddedLength(actualLength uint64) uint64 {
	if actualLength%WordBytes == 0 {
		return actualLength
	}
	return actualLength + (WordBytes - actualLength%WordBytes)
}

func encode() {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	dataLength := uint64(len(data))

	// Simplify the decoder by padding to word length
	padByteCount := paddedLength(dataLength) - dataLength
	if padByteCount != 0 {
		padding := make([]byte, padByteCount)
		data = append(data, padding...)
	}

	if verbose {
		log.Print("dataLength: ", dataLength)
		log.Print("padByteCount: ", padByteCount)
	}

	for i := 0; i < LengthRedundancy; i++ {
		binary.Write(os.Stdout, endian, dataLength)
	}

	for i := uint64(0); i < MaxNumCopies; i++ {
		n, err := os.Stdout.Write(data)
		if n != len(data) {
			log.Printf("Write truncated. Wrote %d complete copies of input data.", i)
			if i < 3 {
				log.Print("WARNING: at least 3 complete copies are necessary for any error protection at all.")
				log.Print("You do not have enough copies. Reduce the amount of input data or use a larger volume.")
				os.Exit(1)
			}
			os.Exit(0)
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("Write completed. Wrote %d complete copies of input data.", uint64(MaxNumCopies))
}

func decodeReadSeeker(f io.ReadSeeker, actualLength uint64) {
	paddedLength := int64(paddedLength(actualLength))

	buf := make([]byte, WordBytes)

	// This will be detected during the first scan.
	var numCopies int64

	seekStart, err := f.Seek(0, 1)
	if err != nil {
		log.Fatal(err)
	}

	// We already read forward by WordBytes bytes, so subtract that from the
	// length when seeking. 
	seekLen := paddedLength - WordBytes

	if verbose2 {
		log.Println("paddedLength: ", paddedLength)
		log.Println("actualLength: ", actualLength)
		log.Println("seekLen: ", seekLen)
	}

ScanLoop:
	for i := 0; ; i++ {
		_, err := io.ReadAtLeast(f, buf, WordBytes)
		if err != nil {
			switch err {
			case io.EOF, io.ErrUnexpectedEOF:
				numCopies = int64(i)
				break ScanLoop
			default:
				log.Fatal(err)
			}
		}
		//if n != WordBytes {
		//	numCopies = int64(i)
		//	break ScanLoop
		//}

		// Note that you can seek beyond the end of a stream without getting an
		// error, which is why we read from it above rather than just seeking.
		_, err = f.Seek(seekLen, 1)
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("Found %d full copies of data on media.", numCopies)

	// TODO: This could be made significantly faster on slow media by storing
	// WordValues{} for a bunch of bytes at a time rather than just one.

	maxj := paddedLength / WordBytes
	for j := int64(0); j < maxj; j++ {
		// Return to beginning of data copies, plus whatever offset we've
		// accumulated
		if _, err := f.Seek(seekStart+j*WordBytes, 0); err != nil {
			log.Fatal(err)
		}

		w := WordValues{}

		for j := int64(0); j < numCopies; j++ {
			var v uint64

			binary.Read(f, endian, &v)
			w.AddWord(v)

			// For debugging
			//binary.Write(os.Stdout, endian, v)

			// We already read forward by 8 bytes, so subtract that from the
			// length when seeking. Apparently you can seek beyond the end of a
			// stream without getting an error, which is why we read from it above
			// rather than just seeking.
			if _, err := f.Seek(seekLen, 1); err != nil {
				log.Fatal(err)
			}
		}

		outputWord := w.DecodeWord()

		if j != maxj-1 || actualLength == uint64(paddedLength) {
			// we can just write the whole word.
			binary.Write(os.Stdout, endian, outputWord)
		} else {
			// last write, and we need to truncate.
			writeBytes := make([]byte, 0, WordBytes)
			writeBuf := bytes.NewBuffer(writeBytes)
			binary.Write(writeBuf, endian, outputWord)
			byteCount := uint64(paddedLength) - actualLength
			writeCount := WordBytes - byteCount
			if verbose2 {
				log.Printf("Writing %d/%d bytes of last word", writeCount, WordBytes)
			}
			os.Stdout.Write(writeBuf.Bytes()[:WordBytes-byteCount])
		}
	}
}

var isDecode = parseArgs()

func parseArgs() bool {
	switch len(os.Args) {
	case 1:
		return false
	case 2:
		switch arg := os.Args[1]; arg {
		case "--help":
			exitDescAndUsage()
		case "-d":
			return true
		default:
			log.Fatalf("Unknown argument '%s' (try --help).", arg)
		}
	default:
		log.Print("Too many arguments.")
		exitUsage()
	}
	panic("Internal error")
}

const appDesc = `encodes to or decodes from a redundant format which is resistant to bit errors.`

func init() {
	log.SetPrefix(os.Args[0] + ":")
	log.SetFlags(log.Lshortfile)
}

func exitDescAndUsage() {
	fmt.Fprintf(os.Stderr, "%s - %s\n\n", os.Args[0], appDesc)
	exitUsage()
}

func exitUsage() {
	fmt.Fprintf(os.Stderr, "USAGE:\n  %s [-d]\n", os.Args[0])
	os.Exit(1)
}

// vim: set fdm=marker:
