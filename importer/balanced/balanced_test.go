package balanced

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	mrand "math/rand"
	"os"
	"testing"

	"github.com/jbenet/go-ipfs/Godeps/_workspace/src/code.google.com/p/go.net/context"
	chunk "github.com/jbenet/go-ipfs/importer/chunk"
	h "github.com/jbenet/go-ipfs/importer/helpers"
	merkledag "github.com/jbenet/go-ipfs/merkledag"
	mdtest "github.com/jbenet/go-ipfs/merkledag/test"
	pin "github.com/jbenet/go-ipfs/pin"
	uio "github.com/jbenet/go-ipfs/unixfs/io"
	u "github.com/jbenet/go-ipfs/util"
)

func buildTestDag(r io.Reader, ds merkledag.DAGService, spl chunk.BlockSplitter) (*merkledag.Node, error) {
	// Start the splitter
	blkch := spl.Split(r)

	dbp := h.DagBuilderParams{
		Dagserv:  ds,
		Maxlinks: h.DefaultLinksPerBlock,
	}

	return BalancedLayout(dbp.New(blkch))
}

//Test where calls to read are smaller than the chunk size
func TestSizeBasedSplit(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	bs := &chunk.SizeSplitter{Size: 512}
	testFileConsistency(t, bs, 32*512)
	bs = &chunk.SizeSplitter{Size: 4096}
	testFileConsistency(t, bs, 32*4096)

	// Uneven offset
	testFileConsistency(t, bs, 31*4095)
}

func dup(b []byte) []byte {
	o := make([]byte, len(b))
	copy(o, b)
	return o
}

func testFileConsistency(t *testing.T, bs chunk.BlockSplitter, nbytes int) {
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, bs)
	if err != nil {
		t.Fatal(err)
	}

	r, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	out, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	err = arrComp(out, should)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuilderConsistency(t *testing.T) {
	nbytes := 100000
	buf := new(bytes.Buffer)
	io.CopyN(buf, u.NewTimeSeededRand(), int64(nbytes))
	should := dup(buf.Bytes())
	dagserv := mdtest.Mock(t)
	nd, err := buildTestDag(buf, dagserv, chunk.DefaultSplitter)
	if err != nil {
		t.Fatal(err)
	}
	r, err := uio.NewDagReader(context.Background(), nd, dagserv)
	if err != nil {
		t.Fatal(err)
	}

	out, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	err = arrComp(out, should)
	if err != nil {
		t.Fatal(err)
	}
}

func arrComp(a, b []byte) error {
	if len(a) != len(b) {
		return fmt.Errorf("Arrays differ in length. %d != %d", len(a), len(b))
	}
	for i, v := range a {
		if v != b[i] {
			return fmt.Errorf("Arrays differ at index: %d", i)
		}
	}
	return nil
}

func TestMaybeRabinConsistency(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	testFileConsistency(t, chunk.NewMaybeRabin(4096), 256*4096)
}

func TestRabinBlockSize(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	buf := new(bytes.Buffer)
	nbytes := 1024 * 1024
	io.CopyN(buf, rand.Reader, int64(nbytes))
	rab := chunk.NewMaybeRabin(4096)
	blkch := rab.Split(buf)

	var blocks [][]byte
	for b := range blkch {
		blocks = append(blocks, b)
	}

	fmt.Printf("Avg block size: %d\n", nbytes/len(blocks))

}

type dagservAndPinner struct {
	ds merkledag.DAGService
	mp pin.ManualPinner
}

func TestIndirectBlocks(t *testing.T) {
	splitter := &chunk.SizeSplitter{512}
	nbytes := 1024 * 1024
	buf := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(buf)

	read := bytes.NewReader(buf)

	ds := mdtest.Mock(t)
	dag, err := buildTestDag(read, ds, splitter)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := uio.NewDagReader(context.Background(), dag, ds)
	if err != nil {
		t.Fatal(err)
	}

	out, err := ioutil.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(out, buf) {
		t.Fatal("Not equal!")
	}
}

func TestSeekingBasic(t *testing.T) {
	nbytes := int64(10 * 1024)
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, &chunk.SizeSplitter{500})
	if err != nil {
		t.Fatal(err)
	}

	rs, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	start := int64(4000)
	n, err := rs.Seek(start, os.SEEK_SET)
	if err != nil {
		t.Fatal(err)
	}
	if n != start {
		t.Fatal("Failed to seek to correct offset")
	}

	out, err := ioutil.ReadAll(rs)
	if err != nil {
		t.Fatal(err)
	}

	err = arrComp(out, should[start:])
	if err != nil {
		t.Fatal(err)
	}
}

func TestSeekToBegin(t *testing.T) {
	nbytes := int64(10 * 1024)
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, &chunk.SizeSplitter{500})
	if err != nil {
		t.Fatal(err)
	}

	rs, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	n, err := io.CopyN(ioutil.Discard, rs, 1024*4)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4096 {
		t.Fatal("Copy didnt copy enough bytes")
	}

	seeked, err := rs.Seek(0, os.SEEK_SET)
	if err != nil {
		t.Fatal(err)
	}
	if seeked != 0 {
		t.Fatal("Failed to seek to beginning")
	}

	out, err := ioutil.ReadAll(rs)
	if err != nil {
		t.Fatal(err)
	}

	err = arrComp(out, should)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSeekToAlmostBegin(t *testing.T) {
	nbytes := int64(10 * 1024)
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, &chunk.SizeSplitter{500})
	if err != nil {
		t.Fatal(err)
	}

	rs, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	n, err := io.CopyN(ioutil.Discard, rs, 1024*4)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4096 {
		t.Fatal("Copy didnt copy enough bytes")
	}

	seeked, err := rs.Seek(1, os.SEEK_SET)
	if err != nil {
		t.Fatal(err)
	}
	if seeked != 1 {
		t.Fatal("Failed to seek to almost beginning")
	}

	out, err := ioutil.ReadAll(rs)
	if err != nil {
		t.Fatal(err)
	}

	err = arrComp(out, should[1:])
	if err != nil {
		t.Fatal(err)
	}
}

func TestSeekEnd(t *testing.T) {
	nbytes := int64(50 * 1024)
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, &chunk.SizeSplitter{500})
	if err != nil {
		t.Fatal(err)
	}

	rs, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	seeked, err := rs.Seek(0, os.SEEK_END)
	if err != nil {
		t.Fatal(err)
	}
	if seeked != nbytes {
		t.Fatal("Failed to seek to end")
	}
}

func TestSeekEndSingleBlockFile(t *testing.T) {
	nbytes := int64(100)
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, &chunk.SizeSplitter{5000})
	if err != nil {
		t.Fatal(err)
	}

	rs, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	seeked, err := rs.Seek(0, os.SEEK_END)
	if err != nil {
		t.Fatal(err)
	}
	if seeked != nbytes {
		t.Fatal("Failed to seek to end")
	}
}

func TestSeekingStress(t *testing.T) {
	nbytes := int64(1024 * 1024)
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, &chunk.SizeSplitter{1000})
	if err != nil {
		t.Fatal(err)
	}

	rs, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	testbuf := make([]byte, nbytes)
	for i := 0; i < 50; i++ {
		offset := mrand.Intn(int(nbytes))
		l := int(nbytes) - offset
		n, err := rs.Seek(int64(offset), os.SEEK_SET)
		if err != nil {
			t.Fatal(err)
		}
		if n != int64(offset) {
			t.Fatal("Seek failed to move to correct position")
		}

		nread, err := rs.Read(testbuf[:l])
		if err != nil {
			t.Fatal(err)
		}
		if nread != l {
			t.Fatal("Failed to read enough bytes")
		}

		err = arrComp(testbuf[:l], should[offset:offset+l])
		if err != nil {
			t.Fatal(err)
		}
	}

}

func TestSeekingConsistency(t *testing.T) {
	nbytes := int64(128 * 1024)
	should := make([]byte, nbytes)
	u.NewTimeSeededRand().Read(should)

	read := bytes.NewReader(should)
	ds := mdtest.Mock(t)
	nd, err := buildTestDag(read, ds, &chunk.SizeSplitter{500})
	if err != nil {
		t.Fatal(err)
	}

	rs, err := uio.NewDagReader(context.Background(), nd, ds)
	if err != nil {
		t.Fatal(err)
	}

	out := make([]byte, nbytes)

	for coff := nbytes - 4096; coff >= 0; coff -= 4096 {
		t.Log(coff)
		n, err := rs.Seek(coff, os.SEEK_SET)
		if err != nil {
			t.Fatal(err)
		}
		if n != coff {
			t.Fatal("wasnt able to seek to the right position")
		}
		nread, err := rs.Read(out[coff : coff+4096])
		if err != nil {
			t.Fatal(err)
		}
		if nread != 4096 {
			t.Fatal("didnt read the correct number of bytes")
		}
	}

	err = arrComp(out, should)
	if err != nil {
		t.Fatal(err)
	}
}
