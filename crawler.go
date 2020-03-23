package adstxt

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Calling remote host error\warning
const (
	errHTTPClientError    = "[%s] remote host [%s] Ads.txt URL [%s]"
	errHTTPGeneralError   = "[%s] remote host [%s] Ads.txt URL [%s]"
	errHTTPBadContentType = "[%s] Ads.txt file content type should be ‘text/plain’ and not [%s]"
)

// parsing error\warning: each error includes Ads.txt remote host (domain level) and explanaiton about the error
const (
	errFailToParseRedirect       = "[%s] failed to parse root domain from HTTP redirect response header. Ads.txt URL [%s] redirect [%s] error [%s]"
	errRedirctToInvalidAdsTxt    = "[%s] failed to get Ads.txt file, redirect from [%s] to invalid Ads.txt URL [%s]"
	errRedirectToDifferentDomain = "Only single redirect out of original root domain scope [%s] is allowed. Additional redirect from [%s] to [%s] is forbidden"
	//errInfiniteRedirect          = "Reached the maximum number of allowed redirects while trying to redirect from [%s]: [%s]"
	errRedirectSameDomain = "Error on redirect: [%s] is redirecting to the same page. Redirecting from [%s] to [%s]"
	errRedirctToMainPage  = "Error on redirect for [%s]: [%s] redirected to [%s] which looks like a homepage"
)

// HTTP crawler settings
const (
	userAgent      = "+https://github.com/ehulsbosch/go-adstxt-crawler"
	requestTimeout = 30
	//maxNumRedirects = 10
)

/*
var (
	redirects = make(map[string]int)
)
*/

// crawler provide methods for downloading Ads.txt files from remote host
type crawler struct {
	client    *http.Client // HTTP client used to make HTTP request for Ads.txt file from remote host
	UserAgent string       // crawler UserAgent string
}

// NewCrawler Create new crawler to fetch Ads.txt file from remote host
func newCrawler() *crawler {
	return &crawler{
		// Create client with required custom parameters.
		// Options: Disable keep-alives, 30sec n/w call timeout, do not follow redirects by default
		client: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
			Timeout: time.Second * requestTimeout,
		},
		UserAgent: userAgent,
	}
}

// send HTTP request to fetch Ads.txt file from remote host
func (c *crawler) sendRequest(req *Request) (*http.Response, error) {
	httpRequest, err := http.NewRequest("GET", req.URL, nil)
	if err != nil {
		return nil, err
	}

	httpRequest.Header.Add("User-Agent", c.UserAgent)
	httpRequest.Header.Add("Accept", "text/plain")
	httpRequest.Header.Add("Accept-Charset", "utf-8")
	httpRequest.Header.Add("Content-Type", "text/plain; charset=utf-8")

	res, err := c.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// handle HTTP redirect resonse: parse new redirect destination from HTTP response header
func (c *crawler) handleRedirect(req *Request, res *http.Response) (string, error) {
	redirect := res.Header.Get("Location")

	// Returning error when redirect is happening to the same location
	if redirect == req.URL {
		return "", fmt.Errorf(errRedirectSameDomain, req.Domain, req.URL, redirect)
	}

	// Increasing the number of redirects for the same url
	//redirects[redirect] += 1

	// Return error when the number of redirects for a single url are reaching a max
	//if redirects[redirect] > maxNumRedirects {
	//	return "", fmt.Errorf(errInfiniteRedirect, req.Domain, req.URL, redirect)
	//}

	log.Printf("[%s]: redirect from [%s] to [%s]", res.Status, req.URL, redirect)

	// Check if redirect destination has the same root domain as the reguest initial root doamin.
	d, err := rootDomain(redirect)
	if err != nil {
		return "", fmt.Errorf(errFailToParseRedirect, req.Domain, req.URL, redirect, err.Error())
	}

	// According to IAB's ads.txt specification, section 3.1 "ACCESS METHOD":
	// "If the server response indicates an HTTP/HTTPS redirect (301, 302, 307 status codes),
	// the advertising system should follow the redirect and consume the data as authoritative for the source of the redirect,
	// if and only if the redirect is within scope of the original root domain as defined above.
	// Multiple redirects are valid as long as each redirect location remains within the original root domain."
	if d != req.Domain {
		// If redirect to different domain, check that this is the first redirect to different domain
		// According to IAB's ads.txt specification, section 3.1 "ACCESS METHOD":
		// "Only a single HTTP redirect to a destination outside the original root domain is allowed to
		// facilitate one-hop delegation of authority to a third party's web server domain."
		prevDomain, _ := rootDomain(req.URL)
		if prevDomain != req.Domain && prevDomain != d {
			return "", fmt.Errorf(errRedirectToDifferentDomain, req.Domain, prevDomain, d)
		}
	}

	// Make sure redirects takes us to another Ads.txt file and not just to home page
	// File doesn't necessarily need to be from a filesystem, so needed more checks for match.
	// Assume when filename equals ads.txt it's coming from filesystem.
	if !strings.HasSuffix(redirect, "/ads.txt") {
		_, err := url.ParseRequestURI(redirect)
		if err != nil {
			return "", fmt.Errorf(errRedirctToInvalidAdsTxt, req.Domain, req.URL, redirect)
		}

		u, err := url.Parse(redirect)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", fmt.Errorf(errRedirctToInvalidAdsTxt, req.Domain, req.URL, redirect)
		}

		if u.Scheme+"://"+u.Hostname() == redirect {
			return "", fmt.Errorf(errRedirctToMainPage, req.Domain, req.URL, redirect)
		}

		return redirect, nil
	}

	return redirect, nil
}

// Read HTTP response body
func (c *crawler) readBody(req *Request, res *http.Response) ([]byte, error) {
	// The HTTP Content-type should be ‘text/plain’, and all other Content-types should be treated as
	// an error and the content ignored
	contentType := res.Header.Get("Content-Type")
	if strings.Index(contentType, "text/plain") != 0 {
		return nil, fmt.Errorf(errHTTPBadContentType, req.URL, contentType)
	}

	// read response body
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// parse Ads.txt file expiration date from the response Expires header
func (c *crawler) parseExpires(res *http.Response) (time.Time, error) {
	expires := res.Header.Get("Expires")
	if len(expires) == 0 {
		return time.Time{}, fmt.Errorf("Failed to parse expires from response header")
	}

	parsedHeader, err := http.ParseTime(expires)
	if err != nil {
		log.Printf("[%s] Error when parsing HTTP expires header from response [%s]", res.Request.URL, err.Error())
		return time.Time{}, err
	}

	return parsedHeader, nil
}
