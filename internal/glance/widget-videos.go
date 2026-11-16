package glance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	videosWidgetPlaylistPrefix = "playlist:"

	youtubeFeedBaseURL               = "https://www.youtube.com/feeds/videos.xml"
	youtubeInnertubeBrowseURL        = "https://www.youtube.com/youtubei/v1/browse?prettyPrint=false"
	youtubeInnertubeClientName       = "WEB"
	youtubeInnertubeClientVersion    = "2.20261112.10.00"
	youtubeInnertubeVideosTabParams  = "EgZ2aWRlb3PyBgQKAjoA"
	youtubeInnertubeVideoContentType = "LOCKUP_CONTENT_TYPE_VIDEO"
)

var (
	videosWidgetTemplate             = mustParseTemplate("videos.html", "widget-base.html", "video-card-contents.html")
	videosWidgetGridTemplate         = mustParseTemplate("videos-grid.html", "widget-base.html", "video-card-contents.html")
	videosWidgetVerticalListTemplate = mustParseTemplate("videos-vertical-list.html", "widget-base.html")
)

type videosWidget struct {
	widgetBase        `yaml:",inline"`
	Videos            videoList `yaml:"-"`
	cachedVideoLists  sync.Map  `yaml:"-"`
	VideoUrlTemplate  string    `yaml:"video-url-template"`
	Style             string    `yaml:"style"`
	CollapseAfter     int       `yaml:"collapse-after"`
	CollapseAfterRows int       `yaml:"collapse-after-rows"`
	Channels          []string  `yaml:"channels"`
	Playlists         []string  `yaml:"playlists"`
	Limit             int       `yaml:"limit"`
	MaxAgeDays        int       `yaml:"max-age-days"`
	VideosPerChannel  int       `yaml:"videos-per-channel"`
	IncludeShorts     bool      `yaml:"include-shorts"`
}

func (w *videosWidget) initialize() error {
	w.withTitle("Videos").withCacheDuration(time.Hour)

	if w.Limit <= 0 {
		w.Limit = 25
	}

	if w.MaxAgeDays < 0 {
		w.MaxAgeDays = 0
	}

	if w.VideosPerChannel < 0 {
		w.VideosPerChannel = 0
	}

	if w.CollapseAfterRows == 0 || w.CollapseAfterRows < -1 {
		w.CollapseAfterRows = 4
	}

	if w.CollapseAfter == 0 || w.CollapseAfter < -1 {
		w.CollapseAfter = 7
	}

	// A bit cheeky, but from a user's perspective it makes more sense when channels and
	// playlists are separate things rather than specifying a list of channels and some of
	// them awkwardly have a "playlist:" prefix
	if len(w.Playlists) > 0 {
		initialLen := len(w.Channels)
		w.Channels = append(w.Channels, make([]string, len(w.Playlists))...)

		for i := range w.Playlists {
			w.Channels[initialLen+i] = videosWidgetPlaylistPrefix + w.Playlists[i]
		}
	}

	return nil
}

func (w *videosWidget) update(ctx context.Context) {
	videos, err := w.fetchYoutubeChannelUploads(w.Channels, w.VideoUrlTemplate, w.IncludeShorts)

	if !w.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	if w.MaxAgeDays > 0 {
		videos = videos.filterNewerThan(time.Duration(w.MaxAgeDays) * 24 * time.Hour)
	}

	if w.VideosPerChannel > 0 {
		videos = videos.limitPerChannel(w.VideosPerChannel)
	}

	if len(videos) > w.Limit {
		videos = videos[:w.Limit]
	}

	w.Videos = videos
}

func (w *videosWidget) Render() template.HTML {
	var template *template.Template

	switch w.Style {
	case "grid-cards":
		template = videosWidgetGridTemplate
	case "vertical-list":
		template = videosWidgetVerticalListTemplate
	default:
		template = videosWidgetTemplate
	}

	return w.renderTemplate(w, template)
}

type youtubeFeedResponseXml struct {
	Channel     string `xml:"author>name"`
	ChannelLink string `xml:"author>uri"`
	Videos      []struct {
		Title     string `xml:"title"`
		Published string `xml:"published"`
		Link      struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`

		Group struct {
			Thumbnail struct {
				Url string `xml:"url,attr"`
			} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
		} `xml:"http://search.yahoo.com/mrss/ group"`
	} `xml:"entry"`
}

func parseYoutubeFeedTime(t string) time.Time {
	parsedTime, err := time.Parse("2006-01-02T15:04:05-07:00", t)
	if err != nil {
		return time.Now()
	}

	return parsedTime
}

func youtubeFeedURL(id string, includeShorts bool) (string, bool) {
	if playlistID, ok := strings.CutPrefix(id, videosWidgetPlaylistPrefix); ok {
		return youtubeFeedBaseURL + "?playlist_id=" + playlistID, true
	}

	if !includeShorts && strings.HasPrefix(id, "UC") {
		playlistID := strings.Replace(id, "UC", "UULF", 1)
		return youtubeFeedBaseURL + "?playlist_id=" + playlistID, false
	}

	return youtubeFeedBaseURL + "?channel_id=" + id, false
}

func (w *videosWidget) fetchYoutubeVideos(id string, videoUrlTemplate string, includeShorts bool) (videoList, error) {
	feedURL, isPlaylist := youtubeFeedURL(id, includeShorts)
	request, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}

	response, rssErr := decodeXmlFromRequest[youtubeFeedResponseXml](defaultHTTPClient, request)
	if rssErr == nil && len(response.Videos) > 0 {
		return youtubeFeedResponseToVideos(response, videoUrlTemplate), nil
	}

	// The official RSS feed is frequently unavailable (returning 404), so fall
	// back to YouTube's public InnerTube API used by the website itself.
	videos, innertubeErr := fetchYoutubeInnertubeVideos(id, isPlaylist, videoUrlTemplate)
	if innertubeErr != nil {
		if rssErr != nil {
			return nil, fmt.Errorf("youtube RSS feed failed: %v; innertube fallback failed: %w", rssErr, innertubeErr)
		}

		return nil, fmt.Errorf("youtube RSS feed returned no videos; innertube fallback failed: %w", innertubeErr)
	}

	return videos, nil
}

func youtubeFeedResponseToVideos(response youtubeFeedResponseXml, videoUrlTemplate string) videoList {
	list := make(videoList, 0, len(response.Videos))

	for j := range response.Videos {
		v := &response.Videos[j]

		var videoUrl string
		if videoUrlTemplate == "" {
			videoUrl = v.Link.Href
		} else {
			parsedUrl, err := url.Parse(v.Link.Href)

			if err == nil {
				videoUrl = strings.ReplaceAll(videoUrlTemplate, "{VIDEO-ID}", parsedUrl.Query().Get("v"))
			} else {
				videoUrl = "#"
			}
		}

		list = append(list, video{
			ThumbnailUrl: v.Group.Thumbnail.Url,
			Title:        v.Title,
			Url:          videoUrl,
			Author:       response.Channel,
			AuthorUrl:    response.ChannelLink + "/videos",
			TimePosted:   parseYoutubeFeedTime(v.Published),
		})
	}

	return list
}

type youtubeInnertubeClient struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
	HL            string `json:"hl"`
}

type youtubeInnertubeRequest struct {
	Context struct {
		Client youtubeInnertubeClient `json:"client"`
	} `json:"context"`
	BrowseID string `json:"browseId"`
	Params   string `json:"params,omitempty"`
}

type youtubeThumbnail struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
}

type youtubeInnertubeText struct {
	SimpleText string `json:"simpleText"`
	Runs       []struct {
		Text string `json:"text"`
	} `json:"runs"`
}

func (t youtubeInnertubeText) text() string {
	if len(t.Runs) > 0 {
		var builder strings.Builder
		for _, run := range t.Runs {
			builder.WriteString(run.Text)
		}

		return builder.String()
	}

	return t.SimpleText
}

type youtubeInnertubeLockup struct {
	ContentID    string `json:"contentId"`
	ContentType  string `json:"contentType"`
	ContentImage struct {
		ThumbnailViewModel struct {
			Image struct {
				Sources []youtubeThumbnail `json:"sources"`
			} `json:"image"`
		} `json:"thumbnailViewModel"`
	} `json:"contentImage"`
	Metadata struct {
		LockupMetadataViewModel struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
			Metadata struct {
				ContentMetadataViewModel struct {
					MetadataRows []struct {
						MetadataParts []struct {
							Text struct {
								Content     string `json:"content"`
								CommandRuns []struct {
									OnTap struct {
										InnertubeCommand struct {
											BrowseEndpoint struct {
												CanonicalBaseURL string `json:"canonicalBaseUrl"`
											} `json:"browseEndpoint"`
										} `json:"innertubeCommand"`
									} `json:"onTap"`
								} `json:"commandRuns"`
							} `json:"text"`
						} `json:"metadataParts"`
					} `json:"metadataRows"`
				} `json:"contentMetadataViewModel"`
			} `json:"metadata"`
		} `json:"lockupMetadataViewModel"`
	} `json:"metadata"`
}

type youtubeInnertubeVideoRenderer struct {
	VideoID           string               `json:"videoId"`
	Title             youtubeInnertubeText `json:"title"`
	PublishedTimeText youtubeInnertubeText `json:"publishedTimeText"`
	Thumbnail         struct {
		Thumbnails []youtubeThumbnail `json:"thumbnails"`
	} `json:"thumbnail"`
	OwnerText      youtubeInnertubeText `json:"ownerText"`
	LongBylineText youtubeInnertubeText `json:"longBylineText"`
}

type youtubeInnertubeContent struct {
	LockupViewModel       *youtubeInnertubeLockup        `json:"lockupViewModel"`
	VideoRenderer         *youtubeInnertubeVideoRenderer `json:"videoRenderer"`
	GridVideoRenderer     *youtubeInnertubeVideoRenderer `json:"gridVideoRenderer"`
	PlaylistVideoRenderer *youtubeInnertubeVideoRenderer `json:"playlistVideoRenderer"`
}

type youtubeInnertubeContents struct {
	RichGridRenderer struct {
		Contents []struct {
			RichItemRenderer struct {
				Content youtubeInnertubeContent `json:"content"`
			} `json:"richItemRenderer"`
		} `json:"contents"`
	} `json:"richGridRenderer"`
	SectionListRenderer struct {
		Contents []struct {
			ItemSectionRenderer struct {
				Contents []youtubeInnertubeContent `json:"contents"`
			} `json:"itemSectionRenderer"`
		} `json:"contents"`
	} `json:"sectionListRenderer"`
}

type youtubeInnertubeBrowseResponse struct {
	Metadata struct {
		ChannelMetadataRenderer struct {
			Title            string `json:"title"`
			ExternalID       string `json:"externalId"`
			ChannelURL       string `json:"channelUrl"`
			VanityChannelURL string `json:"vanityChannelUrl"`
		} `json:"channelMetadataRenderer"`
		PlaylistMetadataRenderer struct {
			Title string `json:"title"`
		} `json:"playlistMetadataRenderer"`
	} `json:"metadata"`
	Contents struct {
		TwoColumnBrowseResultsRenderer struct {
			Tabs []struct {
				TabRenderer struct {
					Title    string                   `json:"title"`
					Selected bool                     `json:"selected"`
					Content  youtubeInnertubeContents `json:"content"`
				} `json:"tabRenderer"`
			} `json:"tabs"`
		} `json:"twoColumnBrowseResultsRenderer"`
	} `json:"contents"`
}

var youtubeRelativeTimePattern = regexp.MustCompile(`(?i)(\d+)\s+(second|minute|hour|day|week|month|year)s?\s+ago`)

func parseYoutubeRelativeTime(text string, now time.Time) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if strings.EqualFold(text, "just now") || strings.EqualFold(text, "now") {
		return now, true
	}

	matches := youtubeRelativeTimePattern.FindStringSubmatch(text)
	if matches == nil {
		return time.Time{}, false
	}

	amount, err := strconv.Atoi(matches[1])
	if err != nil {
		return time.Time{}, false
	}

	var unit time.Duration
	switch strings.ToLower(matches[2]) {
	case "second":
		unit = time.Second
	case "minute":
		unit = time.Minute
	case "hour":
		unit = time.Hour
	case "day":
		unit = 24 * time.Hour
	case "week":
		unit = 7 * 24 * time.Hour
	case "month":
		unit = 30 * 24 * time.Hour
	case "year":
		unit = 365 * 24 * time.Hour
	}

	return now.Add(-time.Duration(amount) * unit), true
}

func youtubeVideoURL(videoID string, videoUrlTemplate string) string {
	if videoUrlTemplate == "" {
		return "https://www.youtube.com/watch?v=" + videoID
	}

	return strings.ReplaceAll(videoUrlTemplate, "{VIDEO-ID}", videoID)
}

func largestYoutubeThumbnail(thumbnails []youtubeThumbnail) string {
	var largest youtubeThumbnail

	for i := range thumbnails {
		if thumbnails[i].URL != "" && thumbnails[i].Width >= largest.Width {
			largest = thumbnails[i]
		}
	}

	return largest.URL
}

func (l *youtubeInnertubeLockup) video(defaultAuthor string, defaultAuthorURL string, videoUrlTemplate string, now time.Time) (video, bool) {
	if l.ContentID == "" || l.ContentType != youtubeInnertubeVideoContentType {
		return video{}, false
	}

	author := defaultAuthor
	authorURL := defaultAuthorURL
	timePosted := now

	for _, row := range l.Metadata.LockupMetadataViewModel.Metadata.ContentMetadataViewModel.MetadataRows {
		for _, part := range row.MetadataParts {
			text := strings.TrimSpace(part.Text.Content)
			if text == "" {
				continue
			}

			if published, ok := parseYoutubeRelativeTime(text, now); ok {
				timePosted = published
				continue
			}

			if author == "" && !strings.Contains(strings.ToLower(text), " view") {
				author = text

				if len(part.Text.CommandRuns) > 0 {
					canonicalURL := part.Text.CommandRuns[0].OnTap.InnertubeCommand.BrowseEndpoint.CanonicalBaseURL
					if canonicalURL != "" {
						authorURL = "https://www.youtube.com" + strings.TrimRight(canonicalURL, "/") + "/videos"
					}
				}
			}
		}
	}

	if authorURL == "" {
		authorURL = "https://www.youtube.com"
	}

	return video{
		ThumbnailUrl: largestYoutubeThumbnail(l.ContentImage.ThumbnailViewModel.Image.Sources),
		Title:        l.Metadata.LockupMetadataViewModel.Title.Content,
		Url:          youtubeVideoURL(l.ContentID, videoUrlTemplate),
		Author:       author,
		AuthorUrl:    authorURL,
		TimePosted:   timePosted,
	}, true
}

func appendYoutubeInnertubeVideo(videos videoList, content youtubeInnertubeContent, defaultAuthor string, defaultAuthorURL string, videoUrlTemplate string, now time.Time) videoList {
	if content.LockupViewModel != nil {
		if v, ok := content.LockupViewModel.video(defaultAuthor, defaultAuthorURL, videoUrlTemplate, now); ok {
			return append(videos, v)
		}
	}

	for _, renderer := range []*youtubeInnertubeVideoRenderer{content.VideoRenderer, content.GridVideoRenderer, content.PlaylistVideoRenderer} {
		if renderer == nil || renderer.VideoID == "" {
			continue
		}

		author := defaultAuthor
		if author == "" {
			author = renderer.OwnerText.text()
			if author == "" {
				author = renderer.LongBylineText.text()
			}
		}

		authorURL := defaultAuthorURL
		if authorURL == "" {
			authorURL = "https://www.youtube.com"
		}

		timePosted := now
		if published, ok := parseYoutubeRelativeTime(renderer.PublishedTimeText.text(), now); ok {
			timePosted = published
		}

		videos = append(videos, video{
			ThumbnailUrl: largestYoutubeThumbnail(renderer.Thumbnail.Thumbnails),
			Title:        renderer.Title.text(),
			Url:          youtubeVideoURL(renderer.VideoID, videoUrlTemplate),
			Author:       author,
			AuthorUrl:    authorURL,
			TimePosted:   timePosted,
		})
	}

	return videos
}

func videosFromYoutubeInnertubeResponse(response youtubeInnertubeBrowseResponse, isPlaylist bool, videoUrlTemplate string) (videoList, error) {
	now := time.Now()
	channel := response.Metadata.ChannelMetadataRenderer
	author := channel.Title
	authorURL := channel.ChannelURL

	if authorURL == "" {
		authorURL = channel.VanityChannelURL
	}
	if authorURL == "" && channel.ExternalID != "" {
		authorURL = "https://www.youtube.com/channel/" + channel.ExternalID
	}
	if authorURL != "" {
		authorURL = strings.TrimRight(authorURL, "/") + "/videos"
	}

	if isPlaylist {
		author = ""
		authorURL = ""
	}

	videos := make(videoList, 0, 15)

	for _, tab := range response.Contents.TwoColumnBrowseResultsRenderer.Tabs {
		for _, item := range tab.TabRenderer.Content.RichGridRenderer.Contents {
			videos = appendYoutubeInnertubeVideo(videos, item.RichItemRenderer.Content, author, authorURL, videoUrlTemplate, now)
		}

		for _, section := range tab.TabRenderer.Content.SectionListRenderer.Contents {
			for _, item := range section.ItemSectionRenderer.Contents {
				videos = appendYoutubeInnertubeVideo(videos, item, author, authorURL, videoUrlTemplate, now)
			}
		}
	}

	if len(videos) == 0 {
		return nil, errNoContent
	}

	return videos, nil
}

func parseYoutubeInnertubeResponse(body []byte, isPlaylist bool, videoUrlTemplate string) (videoList, error) {
	var response youtubeInnertubeBrowseResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	return videosFromYoutubeInnertubeResponse(response, isPlaylist, videoUrlTemplate)
}

func fetchYoutubeInnertubeVideos(id string, isPlaylist bool, videoUrlTemplate string) (videoList, error) {
	browseID := id
	params := youtubeInnertubeVideosTabParams

	if isPlaylist {
		browseID = "VL" + strings.TrimPrefix(id, videosWidgetPlaylistPrefix)
		params = ""
	}

	payload := youtubeInnertubeRequest{
		BrowseID: browseID,
		Params:   params,
	}
	payload.Context.Client = youtubeInnertubeClient{
		ClientName:    youtubeInnertubeClientName,
		ClientVersion: youtubeInnertubeClientVersion,
		HL:            "en",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest(http.MethodPost, youtubeInnertubeBrowseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	setBrowserUserAgentHeader(request)

	response, err := decodeJsonFromRequest[youtubeInnertubeBrowseResponse](defaultHTTPClient, request)
	if err != nil {
		return nil, err
	}

	return videosFromYoutubeInnertubeResponse(response, isPlaylist, videoUrlTemplate)
}

type video struct {
	ThumbnailUrl string
	Title        string
	Url          string
	Author       string
	AuthorUrl    string
	TimePosted   time.Time
}

type videoList []video

func (v videoList) sortByNewest() videoList {
	sort.Slice(v, func(i, j int) bool {
		return v[i].TimePosted.After(v[j].TimePosted)
	})

	return v
}

func (v videoList) filterNewerThan(maxAge time.Duration) videoList {
	cutoff := time.Now().Add(-maxAge)
	filtered := make(videoList, 0, len(v))

	for i := range v {
		if v[i].TimePosted.After(cutoff) {
			filtered = append(filtered, v[i])
		}
	}

	return filtered
}

func (v videoList) limitPerChannel(limit int) videoList {
	counts := make(map[string]int, len(v))
	filtered := make(videoList, 0, len(v))

	for i := range v {
		author := v[i].Author
		if counts[author] >= limit {
			continue
		}

		counts[author]++
		filtered = append(filtered, v[i])
	}

	return filtered
}

func (w *videosWidget) fetchYoutubeChannelUploads(channelOrPlaylistIDs []string, videoUrlTemplate string, includeShorts bool) (videoList, error) {
	task := func(id string) (videoList, error) {
		if cached, ok := w.cachedVideoLists.Load(id); ok {
			entry := cached.(cachedEntry[videoList])
			if time.Since(entry.timestamp) < w.cacheDuration {
				return entry.value, nil
			}
		}

		list, err := w.fetchYoutubeVideos(id, videoUrlTemplate, includeShorts)
		if err != nil {
			cached, ok := w.cachedVideoLists.Load(id)
			if ok {
				return cached.(cachedEntry[videoList]).value, err
			}

			return nil, err
		}

		w.cachedVideoLists.Store(id, cachedEntry[videoList]{value: list, timestamp: time.Now()})
		return list, nil
	}

	job := newJob(task, channelOrPlaylistIDs).withWorkers(30)
	lists, errs, err := workerPoolDo(job)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNoContent, err)
	}

	videos := make(videoList, 0, len(channelOrPlaylistIDs)*15)
	var failed int

	for i := range lists {
		if errs[i] != nil {
			failed++
			slog.Error("Failed to fetch youtube feed", "channel", channelOrPlaylistIDs[i], "error", errs[i])
		}

		// We still append the list even if it failed, because we may have a cached version of the list
		videos = append(videos, lists[i]...)
	}

	if len(videos) == 0 {
		return nil, errNoContent
	}

	videos.sortByNewest()

	if failed > 0 {
		return videos, fmt.Errorf("%w: missing videos from %d channels", errPartialContent, failed)
	}

	return videos, nil
}
