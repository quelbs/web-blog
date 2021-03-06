package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var dataDir = ""
var srcDataDir = filepath.Join("..", "..", "blogimported")
var dstDataDir = filepath.Join("..", "..", "blogdata")

const (
	FormatHtml     = 0
	FormatTextile  = 1
	FormatMarkdown = 2
	FormatText     = 3
)

type Text struct {
	Id        int
	CreatedOn time.Time
	Format    int
	Sha1Str   string
	Sha1      [20]byte
}

var newlines = []byte{'\n', '\n'}
var newline = []byte{'\n'}

func remSep(s string) string {
	return strings.Replace(s, "|", "", -1)
}

// "2006-06-05 17:06:34"
func parseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		log.Fatalf("failed to parse date %s, err: %s", s, err.Error())
	}
	return t
}

type Article struct {
	Id          int
	PublishedOn time.Time
	Permalink1  string
	Permalink2  string
	IsPrivate   bool
	IsDeleted   bool
	Title       string
	Tags        []string
	Versions    []int
}

func parseArticle(d []byte) *Article {
	parts := bytes.Split(d, newline)
	res := &Article{}
	var err error
	for _, p := range parts {
		lp := bytes.SplitN(p, []byte{':', ' '}, 2)
		name := string(lp[0])
		val := string(lp[1])
		if name == "I" {
			if res.Id, err = strconv.Atoi(val); err != nil {
				log.Fatalf("invalid I val: '%s', err: %s\n", val, err.Error())
			}
		} else if name == "On" {
			res.PublishedOn = parseTime(val)
		} else if name == "IS" {
			// do nothing
		} else if name == "P1" {
			res.Permalink1 = strings.TrimSpace(val)
		} else if name == "P2" {
			res.Permalink2 = strings.TrimSpace(val)
			if res.Permalink2 == "None" {
				res.Permalink2 = ""
			}
		} else if name == "P?" {
			// P? == is public
			res.IsPrivate = (val == "False")
		} else if name == "D?" {
			res.IsDeleted = (val == "True")
		} else if name == "T" {
			res.Title = strings.TrimSpace(val)
		} else if name == "TG" {
			res.Tags = strings.Split(val, ",")
		} else if name == "V" {
			versions := strings.Split(val, ",")
			res.Versions = make([]int, len(versions))
			for i, v := range versions {
				if ver, err := strconv.Atoi(v); err != nil {
					log.Fatalf("invalid ver val: '%s', err: %s\n", v, err.Error())
				} else {
					res.Versions[i] = ver
				}
			}
		} else {
			log.Fatalf("Unknown field: '%s'\n", name)
		}
	}
	return res
}

func parseText(d []byte) *Text {
	parts := bytes.Split(d, newline)
	res := &Text{}
	var err error
	for _, p := range parts {
		lp := bytes.SplitN(p, []byte{':', ' '}, 2)
		name := string(lp[0])
		val := string(lp[1])
		if name == "I" {
			if res.Id, err = strconv.Atoi(val); err != nil {
				log.Fatalf("invalid I val: '%s', err: %s\n", val, err.Error())
			}
		} else if name == "M" {
			res.Sha1Str = val
			sha1, err := hex.DecodeString(val)
			if err != nil || len(sha1) != 20 {
				log.Fatalf("error decoding M")
			}
			copy(res.Sha1[:], sha1)
		} else if name == "On" {
			res.CreatedOn = parseTime(val)
		} else if name == "F" {
			if val == "html" {
				res.Format = FormatHtml
			} else if val == "text" {
				res.Format = FormatText
			} else if val == "textile" {
				res.Format = FormatTextile
			} else if val == "markdown" {
				res.Format = FormatMarkdown
			} else {
				log.Fatalf("Unknown F val: '%s'\n", val)
			}
		} else {
			log.Fatalf("Unknown field: '%s'\n", name)
		}
	}
	return res
}

func loadTexts() []*Text {
	filePath := filepath.Join(srcDataDir, "texts.txt")
	d, err := ReadFileAll(filePath)
	if err != nil {
		log.Fatalf("loadTexts(): failed to load %s, error: %s", filePath, err.Error())
	}
	res := make([]*Text, 0)
	for len(d) > 0 {
		idx := bytes.Index(d, newlines)
		if idx == -1 {
			break
		}
		res = append(res, parseText(d[:idx]))
		d = d[idx+2:]
	}
	return res
}

type Crash struct {
	CreatedOn      time.Time
	ProgramName    string
	ProgramVersion string
	IpAddrStr      string
	IpAddrInternal string
	Sha1Str        string
	Sha1           [20]byte
	CrashedLine    string
}

// if it's ipv4 ("a.b.c.d"), convert to number as hex
// otherwise leave alone
func compactIpStr(s string) string {
	var nums [4]uint32
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		// most likely ipv6
		return s
	}
	for n, p := range parts {
		num, _ := strconv.Atoi(p)
		nums[n] = uint32(num)
	}
	n := (nums[0] << 24) | (nums[1] << 16) + (nums[2] << 8) | nums[3]
	return fmt.Sprintf("%x", n)
}

func serCrash(c *Crash) string {
	s1 := base64.StdEncoding.EncodeToString(c.Sha1[:])
	s1 = s1[:len(s1)-1] // remove '=' from the end
	s2 := fmt.Sprintf("%d", c.CreatedOn.Unix())
	s3 := remSep(c.ProgramName)
	s4 := remSep(c.ProgramVersion)
	s5 := c.IpAddrInternal
	s6 := remSep(c.CrashedLine)
	return fmt.Sprintf("C%s|%s|%s|%s|%s|%s\n", s1, s2, s3, s4, s5, s6)
}

func parseCrash(d []byte) *Crash {
	parts := bytes.Split(d, newline)
	res := &Crash{}
	for _, p := range parts {
		lp := bytes.SplitN(p, []byte{':', ' '}, 2)
		name := string(lp[0])
		val := string(lp[1])
		if name == "M" {
			res.Sha1Str = val
			sha1, err := hex.DecodeString(val)
			if err != nil || len(sha1) != 20 {
				log.Fatalf("error decoding M")
			}
			copy(res.Sha1[:], sha1)
		} else if name == "On" {
			res.CreatedOn = parseTime(val)
		} else if name == "Ip" {
			res.IpAddrStr = val
			res.IpAddrInternal = compactIpStr(val)
		} else if name == "N" {
			res.ProgramName = val
		} else if name == "V" {
			res.ProgramVersion = val
		}
	}
	return res
}

func serCrashes(crashes []*Crash) []string {
	res := make([]string, 0)
	for _, c := range crashes {
		res = append(res, serCrash(c))
	}
	return res
}

func loadCrashes() []*Crash {
	filePath := filepath.Join(srcDataDir, "crashes.txt")
	d, err := ReadFileAll(filePath)
	if err != nil {
		log.Fatalf("loadCrashes(): failed to load %s, error: %s", filePath, err.Error())
	}
	res := make([]*Crash, 0)
	for len(d) > 0 {
		idx := bytes.Index(d, newlines)
		if idx == -1 {
			break
		}
		res = append(res, parseCrash(d[:idx]))
		d = d[idx+2:]
	}
	return res
}

func addRedirectIfNeeded(a *Article, redirects *[]ArticleRedirect) {
	realUrl := "article/" + ShortenId(a.Id) + "/" + Urlify(a.Title) + ".html"
	if a.Permalink1 != "" && realUrl != a.Permalink1 {
		//fmt.Printf("'%s' is not equal to permalink1:\n'%s'\n\n", realUrl, a.Permalink1)
		r := ArticleRedirect{a.Permalink1, a.Id}
		*redirects = append(*redirects, r)
	}
	if a.Permalink2 != "" && realUrl != a.Permalink2 {
		//fmt.Printf("'%s' is not equal to permalink2:\n'%s'\n\n", realUrl, a.Permalink2)
		r := ArticleRedirect{a.Permalink2, a.Id}
		*redirects = append(*redirects, r)
	}

}

type ArticleRedirect struct {
	Url       string
	ArticleId int
}

func loadArticles() ([]*Article, []ArticleRedirect) {
	redirects := make([]ArticleRedirect, 0)

	d, err := ReadFileAll(filepath.Join(srcDataDir, "articles.txt"))
	if err != nil {
		log.Fatalf("Failed to load file")
	}
	res := make([]*Article, 0)
	for len(d) > 0 {
		idx := bytes.Index(d, newlines)
		if idx == -1 {
			break
		}
		a := parseArticle(d[:idx])

		addRedirectIfNeeded(a, &redirects)
		res = append(res, a)
		d = d[idx+2:]
	}
	return res, redirects
}

// space saving: false values are empty strings, true is "1"
func boolToStr(b bool) string {
	if b {
		return "1"
	}
	return ""
}

func sanitizeTag(tag string) string {
	return strings.Replace(tag, ",", "", -1)
}

func serTags(tags []string) string {
	s := ""
	lastIdx := len(tags) - 1
	for i, tag := range tags {
		s += sanitizeTag(tag)
		if i != lastIdx {
			s += ","
		}
	}
	return s
}

func serVersions(vers []int) string {
	s := ""
	lastIdx := len(vers) - 1
	for i, ver := range vers {
		s += fmt.Sprintf("%d", ver)
		if i != lastIdx {
			s += ","
		}
	}
	return s
}

func serArticle(a *Article) string {
	s1 := fmt.Sprintf("%d", a.Id)
	s2 := fmt.Sprintf("%d", a.PublishedOn.Unix())
	s3 := remSep(a.Title)
	s4 := boolToStr(a.IsPrivate)
	s5 := boolToStr(a.IsDeleted)
	s6 := serTags(a.Tags)
	s7 := serVersions(a.Versions)
	return fmt.Sprintf("A%s|%s|%s|%s|%s|%s|%s\n", s1, s2, s3, s4, s5, s6, s7)
}

func serText(t *Text) string {
	s1 := fmt.Sprintf("%d", t.CreatedOn.Unix())
	s2 := base64.StdEncoding.EncodeToString(t.Sha1[:])
	s2 = s2[:len(s2)-1] // remove '=' from the end
	return fmt.Sprintf("T%d|%s|%d|%s\n", t.Id, s1, t.Format, s2)
}

func serTextsAndArticles(texts []*Text, articles []*Article) []string {
	res := make([]string, 0)
	for _, t := range texts {
		res = append(res, serText(t))
	}
	for _, t := range articles {
		res = append(res, serArticle(t))
	}
	return res
}

func blobPath(dir, sha1 string) string {
	d1 := sha1[:2]
	d2 := sha1[2:4]
	return filepath.Join(dir, "blobs", d1, d2, sha1)
}

func copyBlobs(texts []*Text) {
	for _, t := range texts {
		sha1 := t.Sha1Str
		srcPath := blobPath(srcDataDir, sha1)
		dstPath := blobPath(dstDataDir, sha1)
		if !PathExists(srcPath) {
			panic("srcPath doesn't exist")
		}
		if !PathExists(dstPath) {
			if err := CreateDirIfNotExists(filepath.Dir(dstPath)); err != nil {
				panic("failed to create dir for dstPath")
			}
			if err := CopyFile(dstPath, srcPath); err != nil {
				log.Fatalf("CopyFile('%s', '%s') failed with %s", dstPath, srcPath, err)
			}
			fmt.Sprintf("%s=>%s\n", srcPath, dstPath)
		}
	}
}

func blobCrashesPath(dir, sha1 string) string {
	d1 := sha1[:2]
	d2 := sha1[2:4]
	return filepath.Join(dir, "blobs_crashes", d1, d2, sha1)
}

func copyFileAddText(dst, src string, s string) error {
	fsrc, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fsrc.Close()
	fdst, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer fdst.Close()
	fdst.WriteString(s)
	if _, err = io.Copy(fdst, fsrc); err != nil {
		return err
	}
	return nil
}

func getCrashPrefixData(crash *Crash) []byte {
	s := fmt.Sprintf("App: %s\n", crash.ProgramName)
	s += fmt.Sprintf("Ip: %s\n", crash.IpAddrStr)
	s += fmt.Sprintf("On: %s\n", crash.CreatedOn.Format(time.RFC3339))
	return []byte(s)
}

func copyCrashesBlobs(crashes []*Crash) {
	for _, c := range crashes {
		srcPath := blobCrashesPath(srcDataDir, c.Sha1Str)
		srcData, err := ReadFileAll(srcPath)
		if err != nil {
			panic("ReadFileAll() failed")
		}
		var buf bytes.Buffer
		buf.Write(getCrashPrefixData(c))
		buf.Write(srcData)
		dstData := buf.Bytes()
		sha1 := Sha1OfBytes(dstData)
		copy(c.Sha1[:], sha1)
		c.Sha1Str = fmt.Sprintf("%x", c.Sha1)

		dstPath := blobCrashesPath(dstDataDir, c.Sha1Str)
		if err := CreateDirIfNotExists(filepath.Dir(dstPath)); err != nil {
			panic("failed to create dir for dstPath")
		}
		if !PathExists(dstPath) {
			WriteBytesToFile(dstData, dstPath)
		}
		c.CrashedLine = ExtractSumatraCrashingLine(dstData)
		fmt.Sprintf("%s=>%s\n", srcPath, dstPath)
	}
}

func verifyData(texts []*Text, articles []*Article) {
	textIdToText := make(map[int]*Text)
	for _, t := range texts {
		textIdToText[t.Id] = t
	}
	for _, a := range articles {
		for _, verId := range a.Versions {
			if _, ok := textIdToText[verId]; !ok {
				log.Fatalf("version id %d from %v not present in textIdToText\n", verId, a)
			}
		}
	}
	fmt.Printf("verifyData(): ok!\n")
}

func saveStrings(filePath string, strs []string) {
	f, err := os.Create(filePath)
	if err != nil {
		log.Fatalf("os.Create() failed with %s", err.Error())
	}
	defer f.Close()
	for _, s := range strs {
		_, err = f.WriteString(s)
		if err != nil {
			log.Fatalf("WriteFile() failed with %s", err.Error())
		}
	}
}

func saveArticleRedirects(filePath string, redirects []ArticleRedirect) {
	f, err := os.Create(filePath)
	if err != nil {
		log.Fatalf("os.Create() failed with %s", err.Error())
	}
	defer f.Close()
	for _, r := range redirects {
		s := fmt.Sprintf("%d|%s\n", r.ArticleId, r.Url)
		_, err = f.WriteString(s)
		if err != nil {
			log.Fatalf("WriteString() failed with %s", err.Error())
		}
	}
}

func renumberTexts(texts []*Text, articles []*Article) {
	oldToNewId := make(map[int]int)
	for i, t := range texts {
		oldToNewId[t.Id] = i
		t.Id = i
	}
	for _, a := range articles {
		for i, verId := range a.Versions {
			if newId, ok := oldToNewId[verId]; ok {
				a.Versions[i] = newId
			} else {
				panic("unknown text version id")
			}
		}
	}
}

func main() {
	if !PathExists(srcDataDir) {
		panic("srcDataDir doesn't exist")
	}
	if !PathExists(dstDataDir) {
		panic("dstDataDir doesn't exist")
	}
	texts := loadTexts()
	articles, redirects := loadArticles()
	crashes := loadCrashes()
	verifyData(texts, articles)
	renumberTexts(texts, articles)
	strs := serTextsAndArticles(texts, articles)
	// must copy before serializing because it updates some values
	copyCrashesBlobs(crashes)
	strCrashes := serCrashes(crashes)

	dataDir := filepath.Join(dstDataDir, "data")
	if err := CreateDirIfNotExists(dataDir); err != nil {
		panic("failed to create dir")
	}

	saveStrings(filepath.Join(dataDir, "blogdata.txt"), strs)
	saveArticleRedirects(filepath.Join(dataDir, "article_redirects.txt"), redirects)
	saveStrings(filepath.Join(dataDir, "crashesdata.txt"), strCrashes)

	copyBlobs(texts)
	fmt.Printf("%d texts, %d articles, %d redirects, %d crashes\n", len(texts), len(articles), len(redirects), len(crashes))
}
