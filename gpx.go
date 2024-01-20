package gpx

import (
	"bytes"
	"encoding/xml"
	"fmt" //errf
	"os"
	"strconv"

	"github.com/pekkizen/numconv"
)

type GPX struct {
	Creator string `xml:"creator,attr"`
	Version string `xml:"version,attr"`
	Time    string `xml:"time"`
	Trks    []Trk  `xml:"trk"`
	errcnt  int
}
type Trk struct {
	Name    string   `xml:"name"`
	Trksegs []Trkseg `xml:"trkseg"`
}
type Trkseg struct {
	Trkpts []Trkpt `xml:"trkpt"`
}
type Trkpt struct {
	Lat float64 `xml:"lat,attr"`
	Lon float64 `xml:"lon,attr"`
	Ele float64 `xml:"ele"`
}

const (
	quotemark       = '"'   // '\'' (single quote) is also accepted by XML parser
	use_std_library = false //for ParseFloat, TrimSpace, Index and IndexByte, testing
)

var (
	latname     = []byte("lat")
	lonname     = []byte("lon")
	eletag      = []byte("<ele>")
	starttag    = []byte("<trkpt")
	closetag    = []byte("</trkpt>")
	trkpLen     int //estimate lenght of a track point slice in bytes
	startSearch int //index from where to start searching for </trkpt>
	errf        = fmt.Errorf
)

// New returns a GPX struct with parsed latitude, longitude and elevation data from gpxFileName.
func New(gpxFileName string, useXMLparser, ignoreErrors bool) (*GPX, error) {

	gpx := &GPX{}
	gpxbytes, e := os.ReadFile(gpxFileName)
	if e != nil {
		return gpx, errf("%v", e)
	}
	if useXMLparser {
		e = xml.Unmarshal(gpxbytes, gpx)
	} else {
		// this is 30 x faster
		e = ParseGPX(gpxbytes, gpx, ignoreErrors)
	}
	if e != nil {
		return gpx, errf("%s: %v", gpxFileName, e)
	}
	return gpx, nil
}

/*
ParseGPX parses lat, lon and ele values of _all_ track points from GPX
file data and builds from the track points a GPX struct with a single track
with a single track segment. Validity of the xml-format is not checked.
A track point error is given if all three numbers are not found.
ParseGPX is 25 x faster than encoding/xml.Unmarshal
*/
func ParseGPX(gpxbytes []byte, gpx *GPX, ignoreErrors bool) error {
	var trkpSlice []byte
	var points int

	gpxbytes, e := selectTrkSegment(gpxbytes)
	if e != nil {
		return e
	}
	points, trkpLen = trkpCountEstimate(gpxbytes)
	startSearch = trkpLen - (len(closetag) + 2)
	trkseg := makeTrkseg(points, gpx)
	trkpnum := 0
	for {
		trkpSlice, gpxbytes = nextTrkpt(gpxbytes)
		if trkpSlice == nil {
			break
		}
		trkp, err := parseTrkpt(trkpSlice)
		switch {
		case err == nil:
			trkpnum++
			*trkseg = append(*trkseg, trkp)
		case ignoreErrors:
			gpx.errcnt++
		default:
			return errf("trackpoint %d: %v", trkpnum+1, err)
		}
	}
	if trkpnum == 0 {
		return errf("No valid trackpoints found")
	}
	clipTrkseg(gpx) //clip excess capacity
	return nil
}

// selectTrkSegment is not implemented yet.
func selectTrkSegment(b []byte) ([]byte, error) {
	d := indexTag(b, starttag)
	if d < 0 {
		return b, errf("No track points found")
	}
	return b[d:], nil //drop everything before first track point
}

/*
nextTrkpt returns the first trackpoint slice of the slice gpxbytes.
nextTrkpt also returnsa a modified gpxbytes, which is the tail of gpxbytes,
when the first track point is removed from it.
Searched track point can be e.g.
<trkpt lon="-5.760211" lat="37.942557"> <ele>615.25</ele> </trkpt>
Returned slice is e.g.
lon="-5.760211" lat="37.942557"> <ele>615.25</ele>
The trackpoint of the returned slice is removed from gpxbytes.
Track point slice can have any other data, unless it is not
disturbing parsing of lat, lon and ele values. So
<trkpt lon "  -5.760211" lat    "37.942557" <ele>615.25<
*/
func nextTrkpt(gpxbytes []byte) (trkpSlice, gpxbytesTail []byte) {
	const startTagLen = 6
	const closeTagLen = 8

	b := gpxbytes
	if len(b) < startSearch {
		return nil, b
	}
	l := indexTag(b, starttag)
	if l < 0 {
		return nil, b
	}
	l += startTagLen + 1 //skip opening tag
	r := startSearch     //skip most data
	d := indexTag(b[r:], closetag)
	if d < 0 {
		return nil, b
	}
	if d > closeTagLen+20 { //missed (or missing) closing tag, retry
		startSearch-- //next time start search from one byte earlier
		r = l + 20
		d = indexTag(b[r:], closetag)
	}
	r += d
	return b[l:r], b[r+closeTagLen:] //drop the first trkpt with closing tag
}

/*
parseTrkpt parses lat, lon and ele values from track point slice b
and returns a track point with these values. Track point slice is
supposed to be like below, attributes lat and lon before elevation.

	lon="-5.760211" lat="37.942557"> <ele>615.25</ele>

White space around numbers is trimmed off and ignored elsewhere.
'+' before number is accepted. Error is given for missing data or
not properly formatted numbers. Errors may come from numconv.Atof,
which is used for parsing numbers.
*/
func parseTrkpt(b []byte) (Trkpt, error) {
	var e1, e2, e3 error
	var point Trkpt

	point.Lon, e1 = parseCoordinate(b, lonname)
	point.Lat, e2 = parseCoordinate(b, latname)
	point.Ele, e3 = parseElevation(b, eletag)
	if e1 == nil {
		e1 = e2
	}
	if e1 == nil {
		e1 = e3
	}
	return point, e1
}

// parseElevatione returns elevation value from the trackpoint slice b.
func parseElevation(b, eletag []byte) (float64, error) {
	const eleKeyLen = 5
	const attribLen = 20

	l := attribLen //skip some lat and lon data
	d := indexTag(b[l:], eletag)
	if d < 0 {
		return 0, errf("missing elevation tag: %s", b)
	}
	l += d + eleKeyLen
	r := indexByte(b[l:], '<') + l //only this, not full </ele>
	if r < l {
		return 0, errf("invalid elevation syntax: %s", b)
	}
	if use_std_library {
		return strconv.ParseFloat(string(bytes.TrimSpace(b[l:r])), 64)
	}
	return numconv.Atof(numconv.Trim(b[l:r]))
}

// parseCoordinate returns the float64 value of latitude or longitude
// koordinate from the trackpoint slice b.
func parseCoordinate(b []byte, name []byte) (float64, error) {
	const nameLen = 3 + 1
	const skipDigits = 4

	l := bytes.Index(b, name) + nameLen
	if l < nameLen {
		return 0, errf("missing "+string(name)+" attribute: %s", b)
	}
	l += indexByte(b[l:], quotemark) + 1
	k := l + skipDigits
	r := indexByte(b[k:], quotemark) + k
	if r < k {
		return 0, errf("missing "+string(name)+" quotemark: %s", b)
	}
	if use_std_library {
		return strconv.ParseFloat(string(bytes.TrimSpace(b[l:r])), 64)
	}
	return numconv.Atof(numconv.Trim(b[l:r]))
}

// Only the first track segment in GPX is used. Even if XML parser
// is used and there are several tracks and segments. ParseGPX puts
// all track points to the first track segment.
func (gpx *GPX) TrkpSlice() []Trkpt {
	return gpx.Trks[0].Trksegs[0].Trkpts
}

func (gpx *GPX) TrkpSliceCopy() []Trkpt {
	s := gpx.Trks[0].Trksegs[0].Trkpts
	return append([]Trkpt{}, s...)
}

func (gpx *GPX) TrkpSliceRelease() {
	gpx.Trks[0].Trksegs[0].Trkpts = nil
}

// clipTrkseg clips excess capacity from the single gpx track segment []Trkpt.
func clipTrkseg(gpx *GPX) {
	s := gpx.Trks[0].Trksegs[0].Trkpts
	gpx.Trks[0].Trksegs[0].Trkpts = s[:len(s):len(s)]
}

func (gpx *GPX) ErrCount() int {
	return gpx.errcnt
}

// trkpCountEstimate estimates the number of track points in GPX data.
func trkpCountEstimate(data []byte) (count, lenght int) {
	const minLen = 24
	if len(data) < 500 {
		return 1, minLen
	}
	s := data[len(data)/2:]
	l := indexTag(s, starttag)
	if l < 0 {
		return 1, minLen
	}
	trkpLen := indexTag(s[l+1:], starttag) + 1
	if trkpLen < 20 {
		return 1, minLen
	}
	return int(float64(len(data)/trkpLen) * 1.0), trkpLen
}

// makeTrkseg initializes *GPX and allocates a track segment of capacity points
// to it, Returns a pointer to track segment.
func makeTrkseg(points int, gpx *GPX) *[]Trkpt {
	gpx.Trks = append(gpx.Trks, Trk{})
	gpx.Trks[0].Trksegs = append(gpx.Trks[0].Trksegs, Trkseg{})
	trkseg := &gpx.Trks[0].Trksegs[0].Trkpts
	*trkseg = make([]Trkpt, 0, points)
	return trkseg
}

// indexByte returns the index of the first instance of c in b,
// or -1 if c is not present in b.
func indexByte(b []byte, c byte) int {
	if use_std_library {
		return bytes.IndexByte(b, c)
	}
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// indexTag returns starting index of XML tag tag []byte.
// Otherwise it is like bytes.Index, but faster for short
// distances and short XML tags: e.g. <trkpt, <ele> and </trkpt>.
// indexTag is inlineable function. Just
func indexTag(b, tag []byte) int {
	if use_std_library {
		return bytes.Index(b, tag)
	}
	j := 0
	for {
		d := indexByte(b[j:], '<')
		j += d
		k := j + len(tag)
		if d < 0 || k > len(b) {
			return -1
		}
		if bytes.Equal(b[j:k], tag) {
			return j
		}
		j += 6
	}
}
