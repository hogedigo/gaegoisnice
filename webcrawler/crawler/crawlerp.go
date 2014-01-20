package crawler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"code.google.com/p/go.net/html"

	"appengine"
	"appengine/datastore"
	"appengine/urlfetch"

	"github.com/mjibson/appstats"
)

func init() {
	http.Handle("/p_crawl", appstats.NewHandler(pcrawlHandler))
}

func pcrawlHandler(c appengine.Context, w http.ResponseWriter, r *http.Request) {
	u := r.FormValue("url")
	depth, err := strconv.Atoi(r.FormValue("depth"))
	if err != nil {
		return
	}

	ch := pcrawl(c, u, depth-1)
	root := <-ch

	b, err := json.Marshal(root)
	if err != nil {
		fmt.Fprintf(w, "error: %s", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func pcrawl(c appengine.Context, aUrl string, depth int) chan *Tree {
	ch := make(chan *Tree)
	go func() {
		if depth > 0 {
			tree := _crawl(c, aUrl, depth)
			ch <- tree
		} else {
			ch <- &Tree{Url: aUrl}
		}
	}()
	return ch
}

func _crawl(c appengine.Context, aUrl string, depth int) *Tree {
	client := urlfetch.Client(c)

	tree := Tree{Url: aUrl}

	c.Infof("fetching... %s", tree.Url)
	resp, err := client.Get(tree.Url)
	if err != nil {
		tree.Error = err.Error()
		return &tree
	}
	c.Infof("fetched ... %s", tree.Url)

	parsedUrl, _ := url.Parse(tree.Url)

	futureChildren := make([]chan *Tree, 0, 5)

	inTitle := false
	z := html.NewTokenizer(resp.Body)
	for {
		tokenType := z.Next()
		switch tokenType {
		case html.ErrorToken:
			c.Infof("eor %s len(children): %d", aUrl, len(futureChildren))
			for i, ch := range futureChildren {
				c.Infof("getting %s child[%d]...", aUrl, i)
				child := <-ch
				c.Infof("got %s child[%d]...", aUrl, i)
				tree.Children = append(tree.Children, child)
				tree.ChildrenUrl = append(tree.ChildrenUrl, child.Url)
			}
			c.Infof("saving... %s", aUrl)
			key := datastore.NewKey(c, "Tree", tree.Url, 0, nil)
			if _, err := datastore.Put(c, key, &tree); err != nil {
				tree.Error = err.Error()
			}
			c.Infof("done... %s", aUrl)
			return &tree
		case html.TextToken:
			if inTitle {
				tree.Title = string(z.Text())
			}
		case html.StartTagToken:
			tagname, _ := z.TagName()
			if len(tagname) == 5 && string(tagname[0:5]) == "title" {
				inTitle = true
			}
			if len(tagname) == 1 && rune(tagname[0]) == 'a' {
				moreAttr := true
				for moreAttr {
					var key, val []byte
					key, val, moreAttr = z.TagAttr()
					c.Infof("child found: %s", val)
					var childUrl string
					if string(key) == "href" {
						if string(val[0:4]) == "http" {
							childUrl = string(val)
						} else {
							childUrl = parsedUrl.Scheme + "://" + parsedUrl.Host + string(val)
						}

						ch := pcrawl(c, childUrl, depth-1)
						futureChildren = append(futureChildren, ch)

						break
					}
				}
			}
		case html.EndTagToken:
			tagname, _ := z.TagName()
			if len(tagname) == 5 && string(tagname[0:5]) == "title" {
				inTitle = false
			}
		default:
		}
	}
}
