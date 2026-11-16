package glance

import (
	"testing"
	"time"
)

func TestYoutubeFeedURL(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		includeShorts bool
		wantURL       string
		wantPlaylist  bool
	}{
		{
			name:          "long form channel",
			id:            "UCXuqSBlHAE6Xw-yeJA0Tunw",
			includeShorts: false,
			wantURL:       "https://www.youtube.com/feeds/videos.xml?playlist_id=UULFXuqSBlHAE6Xw-yeJA0Tunw",
			wantPlaylist:  false,
		},
		{
			name:          "channel with shorts",
			id:            "UCXuqSBlHAE6Xw-yeJA0Tunw",
			includeShorts: true,
			wantURL:       "https://www.youtube.com/feeds/videos.xml?channel_id=UCXuqSBlHAE6Xw-yeJA0Tunw",
			wantPlaylist:  false,
		},
		{
			name:          "playlist",
			id:            videosWidgetPlaylistPrefix + "PL8mG-RkN2uTyZZ00ObwZxxoG_nJbs3qec",
			includeShorts: false,
			wantURL:       "https://www.youtube.com/feeds/videos.xml?playlist_id=PL8mG-RkN2uTyZZ00ObwZxxoG_nJbs3qec",
			wantPlaylist:  true,
		},
		{
			name:          "playlist ignores shorts setting",
			id:            videosWidgetPlaylistPrefix + "PL8mG-RkN2uTyZZ00ObwZxxoG_nJbs3qec",
			includeShorts: true,
			wantURL:       "https://www.youtube.com/feeds/videos.xml?playlist_id=PL8mG-RkN2uTyZZ00ObwZxxoG_nJbs3qec",
			wantPlaylist:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotPlaylist := youtubeFeedURL(tt.id, tt.includeShorts)
			if gotURL != tt.wantURL {
				t.Errorf("youtubeFeedURL() URL = %q, want %q", gotURL, tt.wantURL)
			}
			if gotPlaylist != tt.wantPlaylist {
				t.Errorf("youtubeFeedURL() playlist = %v, want %v", gotPlaylist, tt.wantPlaylist)
			}
		})
	}
}

func TestParseYoutubeRelativeTime(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		text string
		want time.Time
		ok   bool
	}{
		{"13 hours ago", now.Add(-13 * time.Hour), true},
		{"1 day ago", now.Add(-24 * time.Hour), true},
		{"2 weeks ago", now.Add(-14 * 24 * time.Hour), true},
		{"3 months ago", now.Add(-90 * 24 * time.Hour), true},
		{"1 year ago", now.Add(-365 * 24 * time.Hour), true},
		{"Streamed 2 days ago", now.Add(-48 * time.Hour), true},
		{"Premiered 3 weeks ago", now.Add(-21 * 24 * time.Hour), true},
		{"just now", now, true},
		{"728K views", time.Time{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, ok := parseYoutubeRelativeTime(tt.text, now)
			if ok != tt.ok {
				t.Fatalf("parseYoutubeRelativeTime() ok = %v, want %v", ok, tt.ok)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseYoutubeRelativeTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseYoutubeInnertubeResponseChannel(t *testing.T) {
	now := time.Now()
	body := []byte(`{
		"metadata": {
			"channelMetadataRenderer": {
				"title": "Test Channel",
				"externalId": "UCtest",
				"channelUrl": "https://www.youtube.com/channel/UCtest"
			}
		},
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"title": "Videos",
						"selected": true,
						"content": {
							"richGridRenderer": {
								"contents": [
									{"richItemRenderer": {"content": {"lockupViewModel": {
										"contentId": "vid1",
										"contentType": "LOCKUP_CONTENT_TYPE_VIDEO",
										"contentImage": {"thumbnailViewModel": {"image": {"sources": [
											{"url": "https://i.ytimg.com/vi/vid1/hqdefault.jpg", "width": 168},
											{"url": "https://i.ytimg.com/vi/vid1/hq720.jpg", "width": 720}
										]}}},
										"metadata": {"lockupMetadataViewModel": {
											"title": {"content": "First video"},
											"metadata": {"contentMetadataViewModel": {"metadataRows": [
												{"metadataParts": [{"text": {"content": "100K views"}}]},
												{"metadataParts": [{"text": {"content": "2 days ago"}}]}
											]}}
										}}
									}}}},
									{"richItemRenderer": {"content": {"videoRenderer": {
										"videoId": "vid2",
										"title": {"runs": [{"text": "Second video"}]},
										"publishedTimeText": {"simpleText": "3 hours ago"},
										"thumbnail": {"thumbnails": [
											{"url": "https://i.ytimg.com/vi/vid2/hqdefault.jpg", "width": 168}
										]},
										"ownerText": {"runs": [{"text": "Test Channel"}]}
									}}}}
								]
							}
						}
					}
				}]
			}
		}
	}`)

	videos, err := parseYoutubeInnertubeResponse(body, false, "")
	if err != nil {
		t.Fatalf("parseYoutubeInnertubeResponse() error = %v", err)
	}
	if len(videos) != 2 {
		t.Fatalf("parseYoutubeInnertubeResponse() returned %d videos, want 2", len(videos))
	}

	first := videos[0]
	if first.Title != "First video" {
		t.Errorf("videos[0].Title = %q, want %q", first.Title, "First video")
	}
	if first.Url != "https://www.youtube.com/watch?v=vid1" {
		t.Errorf("videos[0].Url = %q", first.Url)
	}
	if first.ThumbnailUrl != "https://i.ytimg.com/vi/vid1/hq720.jpg" {
		t.Errorf("videos[0].ThumbnailUrl = %q, want largest thumbnail", first.ThumbnailUrl)
	}
	if first.Author != "Test Channel" {
		t.Errorf("videos[0].Author = %q, want %q", first.Author, "Test Channel")
	}
	if first.AuthorUrl != "https://www.youtube.com/channel/UCtest/videos" {
		t.Errorf("videos[0].AuthorUrl = %q", first.AuthorUrl)
	}
	if age := now.Sub(first.TimePosted); age < 47*time.Hour || age > 49*time.Hour {
		t.Errorf("videos[0].TimePosted age = %v, want ~48h", age)
	}

	second := videos[1]
	if second.Title != "Second video" {
		t.Errorf("videos[1].Title = %q, want %q", second.Title, "Second video")
	}
	if second.Url != "https://www.youtube.com/watch?v=vid2" {
		t.Errorf("videos[1].Url = %q", second.Url)
	}
	if second.Author != "Test Channel" {
		t.Errorf("videos[1].Author = %q, want %q", second.Author, "Test Channel")
	}
}

func TestParseYoutubeInnertubeResponsePlaylist(t *testing.T) {
	now := time.Now()
	body := []byte(`{
		"metadata": {"playlistMetadataRenderer": {"title": "Test Playlist"}},
		"contents": {
			"twoColumnBrowseResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [{
									"itemSectionRenderer": {
										"contents": [{
											"lockupViewModel": {
												"contentId": "plvid1",
												"contentType": "LOCKUP_CONTENT_TYPE_VIDEO",
												"contentImage": {"thumbnailViewModel": {"image": {"sources": [
													{"url": "https://i.ytimg.com/vi/plvid1/hqdefault.jpg", "width": 168}
												]}}},
												"metadata": {"lockupMetadataViewModel": {
													"title": {"content": "Playlist video"},
													"metadata": {"contentMetadataViewModel": {"metadataRows": [
														{"metadataParts": [{"text": {
															"content": "Test Channel",
															"commandRuns": [{"onTap": {"innertubeCommand": {"browseEndpoint": {
																"canonicalBaseUrl": "/@TestChannel"
															}}}}]
														}}]},
														{"metadataParts": [{"text": {"content": "1 week ago"}}]}
													]}}
												}}
											}
										}]
									}
								}]
							}
						}
					}
				}]
			}
		}
	}`)

	videos, err := parseYoutubeInnertubeResponse(body, true, "https://invidious.example/watch?v={VIDEO-ID}")
	if err != nil {
		t.Fatalf("parseYoutubeInnertubeResponse() error = %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("parseYoutubeInnertubeResponse() returned %d videos, want 1", len(videos))
	}

	got := videos[0]
	if got.Title != "Playlist video" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Url != "https://invidious.example/watch?v=plvid1" {
		t.Errorf("Url = %q, want template applied", got.Url)
	}
	if got.Author != "Test Channel" {
		t.Errorf("Author = %q, want %q", got.Author, "Test Channel")
	}
	if got.AuthorUrl != "https://www.youtube.com/@TestChannel/videos" {
		t.Errorf("AuthorUrl = %q", got.AuthorUrl)
	}
	if age := now.Sub(got.TimePosted); age < 6*24*time.Hour+23*time.Hour || age > 7*24*time.Hour+time.Hour {
		t.Errorf("TimePosted age = %v, want ~168h", age)
	}
}
