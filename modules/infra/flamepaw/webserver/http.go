// Based on https://github.com/gorilla/websocket/blob/master/examples/chat/main.go

// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webserver

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"flamepaw/common"
	"flamepaw/types"
	"fmt"
	"io/ioutil"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	docencoder "encoding/json"

	jsoniter "github.com/json-iterator/go"

	"github.com/Fates-List/discordgo"
	"github.com/davecgh/go-spew/spew"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	ginlogrus "github.com/toorop/gin-logrus"

	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v4/pgxpool"
	log "github.com/sirupsen/logrus"
)

var (
	json          = jsoniter.ConfigCompatibleWithStandardLibrary
	logger        = log.New()
	Docs   string = "# Flamepaw\nFlamepaw is internally used by the bot to provide a RESTful API for tasks requiring high concurrency. The base url is ``https://api.fateslist.xyz/flamepaw``\n\n"
	mutex  sync.Mutex
)

// Given name and docs,
func document(method, route, name, docs string, reqBody interface{}, resBody interface{}) {
	Docs += "## " + strings.Title(strings.ReplaceAll(name, "_", " ")) + "\n\n"
	Docs += "#### " + strings.ToUpper(method) + " " + route + "\n"
	Docs += "**Description:** " + docs + "\n\n"
	var body, err = docencoder.MarshalIndent(resBody, "", "\t")
	if err != nil {
		body = []byte("No documentation available")
	}
	var ibody, err2 = docencoder.MarshalIndent(reqBody, "", "\t")
	if err2 != nil {
		body = []byte("No documentation available")
	}
	Docs += "**Request Body:**\n```json\n" + string(ibody) + "\n```\n\n"
	Docs += "**Response Body:**\n```json\n" + string(body) + "\n```\n\n"
}

// Introduce random delay
const min = 1
const max = 60

func randomDelay() {
	rand.Seed(time.Now().UnixNano())
	time.Sleep(time.Duration(rand.Intn(max-min+1)+min) * time.Millisecond)
}

// API return
func apiReturn(c *gin.Context, statusCode int, done bool, reason interface{}, context interface{}) {
	if reason == "EOF" {
		reason = "Request body required"
	}

	if reason == "" {
		reason = nil
	}

	var ret = gin.H{"done": done, "reason": reason}
	if context != nil {
		ret["ctx"] = context
	}
	c.Header("Content-Type", "application/json")
	body, err := json.MarshalToString(ret)
	if err != nil {
		body, _ = json.MarshalToString(gin.H{"done": false, "reason": "Internal server error: " + err.Error()})
		statusCode = 500
	}
	c.String(statusCode, body)
}

func StartWebserver(db *pgxpool.Pool, redis *redis.Client) {
	hub := newHub(db, redis)
	go hub.run()

	r := gin.New()

	r.Use(ginlogrus.Logger(logger), gin.Recovery())

	document("PPROF", "/pprof", "pprof", "Golang pprof (debugging, may not always exist!)", nil, nil)
	pprof.Register(r, "flamepaw/pprof")

	r.NoRoute(func(c *gin.Context) {
		apiReturn(c, 404, false, "Not Found", nil)
	})
	router := r.Group("/flamepaw")

	document("GET", "/ping", "ping_server", "Ping the server", nil, types.APIResponse{})
	router.GET("/ping", func(c *gin.Context) {
		apiReturn(c, 200, true, nil, nil)
	})

	document("GET", "/__stats", "get_stats", "Get stats of websocket server", nil, nil)
	router.GET("/__stats", func(c *gin.Context) {
		stats := "Websocket server stats:\n\n"
		i := 0
		for client := range hub.clients {
			stats += fmt.Sprintf(
				"Client #%d\nID: %d\nIdentityStatus: %t\nBot: %t\nRLChannel: %s\nSendAll: %t\nSendNone: %t\nMessagePumpUp: %t\nToken: [redacted] \n\n\n",
				i,
				client.ID,
				client.IdentityStatus,
				client.Bot,
				client.RLChannel,
				client.SendAll,
				client.SendNone,
				client.MessagePumpUp,
			)
			i++
		}
		c.String(200, stats)
	})

	document("POST", "/github", "github_webhook", "Post to github webhook. Needs authorization", types.GithubWebhook{}, types.APIResponse{})
	router.POST("/github", func(c *gin.Context) {
		var bodyBytes []byte
		if c.Request.Body != nil {
			bodyBytes, _ = ioutil.ReadAll(c.Request.Body)
		}

		// Restore the io.ReadCloser to its original state
		c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

		var signature = c.Request.Header.Get("X-Hub-Signature-256")

		mac := hmac.New(sha256.New, []byte(common.GHWebhookSecret))
		mac.Write([]byte(bodyBytes))
		expected := hex.EncodeToString(mac.Sum(nil))

		if "sha256="+expected != signature {
			log.Error(expected + " " + signature + " ")
			apiReturn(c, 401, false, "Invalid signature", nil)
			return
		}

		var gh types.GithubWebhook
		err := c.BindJSON(&gh)
		if err != nil {
			log.Error(err)
			apiReturn(c, 422, false, err.Error(), nil)
			return
		}

		var header = c.Request.Header.Get("X-GitHub-Event")

		var messageSend discordgo.MessageSend

		if header == "push" {
			var commitList string
			for _, commit := range gh.Commits {
				commitList += fmt.Sprintf("%s [%s](%s) | [%s](%s)\n", commit.Message, commit.ID[:7], commit.URL, commit.Author.Username, "https://github.com/"+commit.Author.Username)
			}

			messageSend = discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{
					{
						Color: 0x00ff1a,
						URL:   gh.Repo.URL,
						Author: &discordgo.MessageEmbedAuthor{
							Name:    gh.Sender.Login,
							IconURL: gh.Sender.AvatarURL,
						},
						Title: "Push on " + gh.Repo.FullName,
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:  "Branch",
								Value: "**Ref:** " + gh.Ref + "\n" + "**Base Ref:** " + gh.BaseRef,
							},
							{
								Name:  "Commits",
								Value: commitList,
							},
							{
								Name:  "Pusher",
								Value: fmt.Sprintf("[%s](%s)", gh.Pusher.Name, "https://github.com/"+gh.Pusher.Name),
							},
						},
					},
				},
			}
		} else if header == "star" {
			var color int
			var title string
			if gh.Action == "created" {
				color = 0x00ff1a
				title = "Starred: " + gh.Repo.FullName
			} else {
				color = 0xff0000
				title = "Unstarred: " + gh.Repo.FullName
			}
			messageSend = discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{
					{
						Color: color,
						URL:   gh.Repo.URL,
						Title: title,
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:  "User",
								Value: "[" + gh.Sender.Login + "]" + "(" + gh.Sender.HTMLURL + ")",
							},
						},
					},
				},
			}
		} else if header == "issues" {
			var body string = gh.Issue.Body
			if len(gh.Issue.Body) > 1000 {
				body = gh.Issue.Body[:1000]
			}

			if body == "" {
				body = "No description available"
			}

			var color int
			if gh.Action == "deleted" || gh.Action == "unpinned" {
				color = 0xff0000
			} else {
				color = 0x00ff1a
			}

			messageSend = discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{
					{
						Color: color,
						URL:   gh.Issue.HTMLURL,
						Author: &discordgo.MessageEmbedAuthor{
							Name:    gh.Sender.Login,
							IconURL: gh.Sender.AvatarURL,
						},
						Title: fmt.Sprintf("Issue %s on %s (#%d)", gh.Action, gh.Repo.FullName, gh.Issue.Number),
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:  "Action",
								Value: gh.Action,
							},
							{
								Name:  "User",
								Value: fmt.Sprintf("[%s](%s)", gh.Sender.Login, gh.Sender.HTMLURL),
							},
							{
								Name:  "Title",
								Value: gh.Issue.Title,
							},
							{
								Name:  "Body",
								Value: body,
							},
						},
					},
				},
			}
		} else if header == "pull_request" {
			var body string = gh.PullRequest.Body
			if len(gh.PullRequest.Body) > 1000 {
				body = gh.PullRequest.Body[:1000]
			}

			if body == "" {
				body = "No description available"
			}

			var color int
			if gh.Action == "closed" {
				color = 0xff0000
			} else {
				color = 0x00ff1a
			}

			messageSend = discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{
					{
						Color: color,
						URL:   gh.PullRequest.HTMLURL,
						Author: &discordgo.MessageEmbedAuthor{
							Name:    gh.Sender.Login,
							IconURL: gh.Sender.AvatarURL,
						},
						Title: fmt.Sprintf("Pull Request %s on %s (#%d)", gh.Action, gh.Repo.FullName, gh.PullRequest.Number),
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:  "Action",
								Value: gh.Action,
							},
							{
								Name:  "User",
								Value: fmt.Sprintf("[%s](%s)", gh.Sender.Login, gh.Sender.HTMLURL),
							},
							{
								Name:  "Title",
								Value: gh.PullRequest.Title,
							},
							{
								Name:  "Body",
								Value: body,
							},
							{
								Name:  "More Information",
								Value: fmt.Sprintf("**Base Ref:** %s\n**Base Label:** %s\n**Head Ref:** %s\n**Head Label:** %s", gh.PullRequest.Base.Ref, gh.PullRequest.Base.Label, gh.PullRequest.Head.Ref, gh.PullRequest.Head.Label),
							},
						},
					},
				},
			}
		} else if header == "issue_comment" {
			var body string = gh.Issue.Body
			if len(gh.Issue.Body) > 1000 {
				body = gh.Issue.Body[:1000]
			}

			if body == "" {
				body = "No description available"
			}

			var color int
			if gh.Action == "deleted" {
				color = 0xff0000
			} else {
				color = 0x00ff1a
			}

			messageSend = discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{
					{
						Color: color,
						URL:   gh.Issue.HTMLURL,
						Author: &discordgo.MessageEmbedAuthor{
							Name:    gh.Sender.Login,
							IconURL: gh.Sender.AvatarURL,
						},
						Title: fmt.Sprintf("Comment on %s (#%d) %s", gh.Repo.FullName, gh.Issue.Number, gh.Action),
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:  "User",
								Value: fmt.Sprintf("[%s](%s)", gh.Sender.Login, gh.Sender.HTMLURL),
							},
							{
								Name:  "Title",
								Value: gh.Issue.Title,
							},
							{
								Name:  "Body",
								Value: body,
							},
						},
					},
				},
			}
		} else if header == "pull_request_review_comment" {
			var body string = gh.PullRequest.Body
			if len(gh.PullRequest.Body) > 1000 {
				body = gh.PullRequest.Body[:1000]
			}

			if body == "" {
				body = "No description available"
			}

			var color int
			if gh.Action == "deleted" {
				color = 0xff0000
			} else {
				color = 0x00ff1a
			}

			messageSend = discordgo.MessageSend{
				Embeds: []*discordgo.MessageEmbed{
					{
						Color: color,
						URL:   gh.PullRequest.HTMLURL,
						Author: &discordgo.MessageEmbedAuthor{
							Name:    gh.Sender.Login,
							IconURL: gh.Sender.AvatarURL,
						},
						Title: "Pull Request Review Comment on " + gh.Repo.FullName + " (#" + strconv.Itoa(gh.PullRequest.Number) + ")",
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:  "User",
								Value: fmt.Sprintf("[%s](%s)", gh.Sender.Login, gh.Sender.HTMLURL),
							},
							{
								Name:  "Title",
								Value: gh.PullRequest.Title,
							},
							{
								Name:  "Body",
								Value: body,
							},
						},
					},
				},
			}
		} else {
			messageSend = discordgo.MessageSend{
				Content: "**Action: " + header + "**",
				TTS:     false,
				File: &discordgo.File{
					Name:        "gh-event.txt",
					ContentType: "application/octet-stream",
					Reader:      strings.NewReader(spew.Sdump(gh)),
				},
			}
		}

		_, err = common.DiscordMain.ChannelMessageSendComplex(common.GithubChannel, &messageSend)

		if err != nil {
			log.Error(err)
			apiReturn(c, 400, false, "Error sending message: "+err.Error(), nil)
			return
		}

		apiReturn(c, 200, true, nil, nil)
	})

	document("WS", "/ws", "websocket", "The websocket gateway for Fates List", nil, nil)
	router.GET("/ws", func(c *gin.Context) {
		serveWs(hub, c.Writer, c.Request)
	})

	err := r.RunUnix("/home/meow/fatesws.sock")
	if err != nil {
		log.Fatal("could not start listening: ", err)
		return
	}
}
