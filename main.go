package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmcvetta/neoism"
	"github.com/pebbe/go-proj-4/proj"
)

func removeWhitespaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func fastSplit(c rune) bool {
	return c == ' '
}

type Geometry struct {
	Pos         []string `xml:"pos"`
	PosList     string   `xml:"posList"`
	Coordinates string   `xml:"coordinates"`
}

type Building struct {
	ID               string     `xml:"id,attr"`
	LOD0FootPrint    []Geometry `xml:"lod0FootPrint>MultiSurface>surfaceMember>Polygon>exterior>LinearRing"`
	LOD1MultiSurface []Geometry `xml:"boundedBy>GroundSurface>lod1MultiSurface>MultiSurface>surfaceMember>Polygon>exterior>LinearRing"`
	LOD2MultiSurface []Geometry `xml:"boundedBy>GroundSurface>lod2MultiSurface>MultiSurface>surfaceMember>Polygon>exterior>LinearRing"`
	LOD3MultiSurface []Geometry `xml:"boundedBy>GroundSurface>lod3MultiSurface>MultiSurface>surfaceMember>Polygon>exterior>LinearRing"`
	LOD4MultiSurface []Geometry `xml:"boundedBy>GroundSurface>lod4MultiSurface>MultiSurface>surfaceMember>Polygon>exterior>LinearRing"`
}

var wgs84, _ = proj.NewProj("+init=epsg:4326")

func (b *Building) GeometryAsWKT() string {
	var wkt string
	if len(b.LOD0FootPrint) > 0 {
		wkt = toWKT(b.LOD0FootPrint)
	} else if len(b.LOD1MultiSurface) > 0 {
		wkt = toWKT(b.LOD1MultiSurface)
	} else if len(b.LOD2MultiSurface) > 0 {
		wkt = toWKT(b.LOD2MultiSurface)
	} else if len(b.LOD3MultiSurface) > 0 {
		wkt = toWKT(b.LOD3MultiSurface)
	} else if len(b.LOD4MultiSurface) > 0 {
		wkt = toWKT(b.LOD4MultiSurface)
	}
	return wkt
}

func toWKT(g []Geometry) string {
	wkt := "MULTIPOLYGON("
	for _, p := range g {
		poly := "(("
		if len(p.Pos) > 0 { // pos done!
			for _, s := range p.Pos {
				s = removeWhitespaces(s)
				sa := strings.Split(s, " ")
				poly = poly + sa[0] + " " + sa[1] + ", "
			}
		} else if p.PosList != "" { // posList done!
			s := p.PosList
			sa := strings.Split(s, " ")
			for i, c := range sa {
				if (i+1)%3 != 0 {
					poly = poly + c + " "
				} else {
					poly = poly[:len(poly)-1] + ", "
				}
			}
		} else if p.Coordinates != "" { // coordinates - deprecated but still should be implemented
			s := removeWhitespaces(p.Coordinates)
			sa := strings.Split(s, " ")
			for _, c := range sa {
				ca := strings.Split(c, ",")
				poly = poly + ca[0] + " " + ca[1] + ", "
			}
		}
		poly = poly[:len(poly)-2] + "))"
		wkt = wkt + poly
	}
	wkt = wkt + ")"
	return wkt
}

func transform(s, epsg string) string {
	projection, _ := proj.NewProj("+init=epsg:" + epsg)
	re := regexp.MustCompile(`\(\((.*?)\)\)`) // everyrthing between "((" and "))", some "(" remain
	wkt := "MULTIPOLYGON("
	for _, i := range re.FindAllString(s, -1) {
		poly := strings.Trim(i, "()") // remove all "(" and ")"
		sa := strings.Split(poly, ",")
		p := "(("
		for _, c := range sa {
			ca := strings.FieldsFunc(c, fastSplit) // no split because it leaves empty strings
			x, _ := strconv.ParseFloat(ca[0], 64)
			y, _ := strconv.ParseFloat(ca[1], 64)
			xt, yt, _ := proj.Transform2(projection, wgs84, x, y)
			xts := strconv.FormatFloat(proj.RadToDeg(xt), 'f', 6, 64)
			yts := strconv.FormatFloat(proj.RadToDeg(yt), 'f', 6, 64)
			ct := xts + " " + yts
			p = p + ct + ", "
		}
		p = p[:len(p)-2] + "))"
		wkt = wkt + p + ", "
	}
	wkt = wkt[:len(wkt)-2] + ")"

	return wkt
}

func createNode(db *neoism.Database, props neoism.Props, label string, id string, layer string) {
	n, _ := db.CreateNode(props)
	n.AddLabel(label)
	addSpatialIndex(db, id, layer)
}

func addSpatialIndex(db *neoism.Database, id string, layer string) {
	cq := neoism.CypherQuery{
		Statement: `
			MATCH (n)
			WHERE EXISTS(n.geometry) AND n.id={id}
			WITH collect(n) AS nodes
			CALL spatial.addNodes({layer}, nodes)
			YIELD count RETURN count
		`,
		Parameters: neoism.Props{"id": id, "layer": layer},
	}

	err := db.Cypher(&cq)
	_ = err
}

func main() {
	pathPtr := flag.String("path", "none", "Path to the file.")
	epsgPtr := flag.String("epsg", "none", "CRS EPSG id number.")
	flag.Parse()

	if *pathPtr != "none" {
		conn, _ := neoism.Connect("http://neo4j:test@localhost:7474/db/data")
		r, _ := os.Open(*pathPtr)
		d := xml.NewDecoder(r)
		for {
			t, tokenErr := d.Token()
			if tokenErr != nil {
				if tokenErr == io.EOF {
					break
				}
			}
			switch t := t.(type) {
			case xml.StartElement:
				if t.Name.Local == "Building" {
					var b Building
					if err := d.DecodeElement(&b, &t); err != nil {
						panic(err)
					}
					// do something with the object
					props := neoism.Props{}
					props["id"] = b.ID
					props["geometry"] = transform(b.GeometryAsWKT(), *epsgPtr)

					createNode(conn, props, "Building", b.ID, "buildings")
				}
			}
		}
	} else {
		fmt.Println("Not enough arguments. Expected 2: path, epsg.")
	}
}
