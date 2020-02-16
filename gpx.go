package gpx

import (
	"encoding/xml"
	"io/ioutil"
	"fmt"		//Errorf
	"bytes"

	"numconv"
)

//GPX --
type GPX struct {
	Creator string 	`xml:"creator,attr"`
	Version string 	`xml:"version,attr"`
	Time    string 	`xml:"time"`
	Trks     []Trk  `xml:"trk"`
	Errcnt	int
}
//Trk --
type Trk struct {
	Name   string		`xml:"name"`
	Trksegs []Trkseg 	`xml:"trkseg"`
}
//Trkseg --
type Trkseg struct {
	Trkpts []Trkpt `xml:"trkpt"`
}
//Trkpt <trkpt lon="-5.760211" lat="37.942557"> <ele>615.25</ele> </trkpt>
type Trkpt struct {
	Lat float64 `xml:"lat,attr"`
	Lon float64 `xml:"lon,attr"`
	Ele float64 `xml:"ele"`
}
var quotemark byte 
var errf = fmt.Errorf
var parseFloat = numconv.Atof

var (
	latkey 		= []byte("lat")
	lonkey 		= []byte("lon")
	elekey 		= []byte("<ele>")
	trkpstart	= []byte("<trkpt")
	trkpclose	= []byte("</trkpt>")
)

//New returns GPX struct with parsed latitude, longitude and elevation data from gpxfile.
func New(gpxfile string, useXMLparser, ignoreErrors bool) (*GPX, error) {

	gpx := &GPX{}
	gpxbytes, e := ioutil.ReadFile(gpxfile);
	if e != nil || len(gpxbytes) < 100 {
		return gpx, errf("%s: %v", gpxfile, e)
	}
	if useXMLparser {
		e = xml.Unmarshal(gpxbytes, &gpx)
	} else {
		e = ParseGPX(gpxbytes, gpx, ignoreErrors)
	}
	if  e != nil {
		return gpx, errf("%s: %v", gpxfile, e)
	}
	return gpx, nil
}


//TrkpCount ----
func (gpx *GPX) TrkpCount() int {
	i := 0
	for _, trk := range gpx.Trks {
		for _, seg := range trk.Trksegs {
			i += len(seg.Trkpts)
		}
	}
	return i
}

// fmt.Println("parseGPX/trkp estimate: ", trkpCountEstimate(data))

// ParseGPX parses lat, lon and ele values of all track points from GPX file data gpxbytes 
// and builds from track points a GPX struct with single track with single track segment.
// Validity of the xml-format is not checked. Single or double quotation marks are ok, 
// but not both in same file. Track point error is given if all three numbers are not found.
// ParseGPX is nearly 20 x faster than encoding/xml.Unmarshal
//
func ParseGPX(gpxbytes []byte, gpx *GPX, ignoreErrors bool) error {
	var err error
	if quotemark, err = getQuotemark(gpxbytes); err != nil {
		return err
	}

	gpx.Trks = append(gpx.Trks, Trk{})
	gpx.Trks[0].Trksegs = append(gpx.Trks[0].Trksegs, Trkseg{})
	trkseg := &gpx.Trks[0].Trksegs[0].Trkpts
	*trkseg = make([]Trkpt, 0, trkpCountEstimate(gpxbytes))

	trkp := Trkpt{}
	trkpnum := 0

	for {
		trkpSlice := nextTrkpt(&gpxbytes)
		if trkpSlice == nil {
			break
		}
		if err := parseTrkpt(trkpSlice, &trkp); err != nil {
			if ignoreErrors {
				gpx.Errcnt++
				continue
			}
			return errf("trackpoint %d: %v", trkpnum + 1, err)
		}
		trkpnum++
		*trkseg = append(*trkseg, trkp)
	}
	if trkpnum == 0 {
		return errf("No valid trackpoints found")
	}
	return nil
}

// nextTrkpt returns the first trackpoint slice of slice gpxbytes. Searched track point eg.
//		<trkpt lon="-5.760211" lat="37.942557"> <ele>615.25</ele> </trkpt>
// Returned slice is eg.
// 		lon="-5.760211" lat="37.942557"> <ele>615.25</ele>
// Returned slice is removed from gpxbytes.
//
func nextTrkpt(gpxbytes *[]byte) []byte {

	b := *gpxbytes
	d := bytes.Index(b, trkpstart)
	if d < 0 { 
		return nil
	}
	l := d + 7	 //skip opening tag
	r := l + 35  //skip some data
	d = bytes.Index(b[r:], trkpclose)
	if d < 0 {
		return nil 
	}
	r += d
	*gpxbytes = b[r+8:] //remove with xml closing tag
	return b[l:r]
}

// getQuotemark returns quotemark (" or ') from first trackpoint.
func getQuotemark(data []byte) (byte, error) {

	s := nextTrkpt(&data)
	d1 := bytes.IndexByte(s, '"') 
	d2 := bytes.IndexByte(s, '\'')

	if d1 > 0 && d2 < 0 {
		return '"', nil
	}
	if d1 < 0 && d2 > 0 {
		return '\'', nil
	}
	if d1 < 0 && d2 < 0 {
		return 0, errf("Missing lat/lon values quote marking: %s", s )
	}
	if d1 < d2 {
		return '"', nil
	}
	return '\'', nil
}

func trkpCountEstimate(data []byte) int {
	if len(data) < 500 {
		return 1
	}
	s := data[len(data) / 2:]
	i := bytes.Index(s, []byte("<trkpt"))
	if i < 0 { 
		return 1
	}
	d := bytes.Index(s[i+1:], []byte("<trkpt")) + 1
	if d < 20 {
		return 1
	}
	return int(float64(len(data) / d) * 1.05)
}

// parseTrkpt parses lat, lon and ele values from track point slice b 
// and sets these to track point struct trkp. Track point slice is eg.
// 		lon="-5.760211" lat="37.942557"> <ele>615.25</ele> 
// White space around numbers is trimmed off and ignored elsewhere. 
// '+' before number is accepted. Error is given for missing data or
// not properly formatted numbers.
//
func parseTrkpt(b []byte, trkp *Trkpt) error {
	var e1, e2, e3 error

	trkp.Lat, e1 = parseLatitude(b)
	trkp.Lon, e2 = parseLongitude(b)
	trkp.Ele, e3 = parseElevation(b)
	if e1 == nil {
		e1 = e2
	}
	if e1 == nil {
		e1 = e3
	}
	return e1
}

// parseElevatione returns float64 value of elevation from
// trackpoint slice b.
func parseElevation(b []byte) (float64, error) {

	i := bytes.IndexByte(b, '<')  //skip attributes
	if i < 0 {
		i = 0
	}
	d := bytes.Index(b[i:], elekey) + 5
	if  d < 5 {
		return 0, errf("missing elevation: %s", b)
	}
	s := b[i+d:]
	d = bytes.IndexByte(s, '<') //just this, not full </ele>
	if d < 0 {
		return 0, errf("invalid elevation syntax: %s", b)
	}
	return  parseFloat(s[:d])
}

// parseLatitude returns float64 value of latitude koordinate from
// trackpoint slice b.
func parseLatitude(b []byte) (float64, error) {

	i := bytes.Index(b, latkey) + 4
	if i < 4 {
		return 0, errf("missing lat: %s", b)
	}
	i += bytes.IndexByte(b[i:], quotemark) + 1
	s := b[i:]
	d := bytes.IndexByte(s, quotemark) 
	if d < 0 {
		return 0, errf("missing lat quotemark: %s", b)
	}
	return  parseFloat(s[:d])
}
// package internal/bytealg: indexbyte_generic.go
func IndexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// parseLongitude returns float64 value of londitude koordinate from
// trackpoint slice b.
func parseLongitude(b []byte) (float64, error) {

	i := bytes.Index(b, lonkey) + 4
	if i < 4 {
		return 0, errf("missing lon: %s", b)
	}
	i += bytes.IndexByte(b[i:], quotemark) + 1
	s := b[i:]
	d := bytes.IndexByte(s, quotemark) 
	if d < 0 {
		return 0, errf("missing lon quotemark: %s", b)
	}
	return  parseFloat(s[:d])
}