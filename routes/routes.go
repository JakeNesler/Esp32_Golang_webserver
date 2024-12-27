package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const esp32BaseURL = "http://192.168.1.162"

func setPattern(pattern, duration, colors string) error {
	fmt.Printf("setPattern invoked with pattern=%s, duration=%s, colors=%s\n", pattern, duration, colors)

	resp, err := http.PostForm(esp32BaseURL+"/pattern", map[string][]string{
		"pattern":  {pattern},
		"duration": {duration},
		"colors":   {colors},
	})
	if err != nil {
		return fmt.Errorf("failed to set pattern '%s': %v", pattern, err)
	}
	defer resp.Body.Close()

	fmt.Printf("ESP32 Response Status: %s\n", resp.Status)
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read ESP32 response body: %v", err)
	}
	fmt.Printf("ESP32 Response Body: %s\n", string(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ESP32 returned non-2xx status code: %d, body: %s", resp.StatusCode, respBody)
	}
	return nil
}

func turnOff() error {
	fmt.Println("Turning off lights...")
	resp, err := http.Post(esp32BaseURL+"/off", "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to turn off lights: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("ESP32 /off Response: %s\n", string(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ESP32 returned non-2xx status code: %d, body: %s", resp.StatusCode, respBody)
	}
	return nil
}

// RunServers starts two servers:
// 1) Main API on :8080 (for /api/pattern, /api/off, GET /)
// 2) Webhook server on :9091 (for /webhook/plex)
func RunServers() {
	//------------------------------------------------
	// MAIN SERVER (Port 8080)
	//------------------------------------------------
	go func() {
		mainRouter := gin.Default()

		// Simple Home Route
		mainRouter.GET("/", func(c *gin.Context) {
			c.String(http.StatusOK, "Welcome to the Home Page on :8080!")
		})

		// POST /api/pattern - sets a pattern on the ESP32
		mainRouter.POST("/api/pattern", func(c *gin.Context) {
			pattern := c.PostForm("pattern")
			duration := c.PostForm("duration")
			colors := c.PostForm("colors")

			if err := setPattern(pattern, duration, colors); err != nil {
				fmt.Printf("❌ Failed to set pattern: %v\n", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("Failed to set pattern: %v", err),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "Pattern updated successfully"})
		})

		// POST /api/off - turns off the lights
		mainRouter.POST("/api/off", func(c *gin.Context) {
			if err := turnOff(); err != nil {
				fmt.Printf("❌ Failed to turn off lights: %v\n", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("Failed to turn off lights: %v", err),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "Lights turned off"})
		})

		fmt.Println("🎄 Starting main server on http://localhost:8080 🎄")
		if err := mainRouter.Run(":8080"); err != nil {
			fmt.Printf("❌ Failed to start main server: %v\n", err)
		}
	}()

	webhookRouter := gin.Default()
	webhookRouter.POST("/webhook/plex", func(c *gin.Context) {
		fmt.Println("📡 Incoming Webhook Request:")
		fmt.Printf("🔗 Method: %s\n", c.Request.Method)
		fmt.Printf("📍 URL: %s\n", c.Request.URL)
		fmt.Printf("📝 Headers: %+v\n", c.Request.Header)
		fmt.Printf("🔄 Content-Type: %s\n", c.ContentType())

		var (
			payloadBytes []byte
			err          error
		)

		switch c.ContentType() {
		case "application/json":
			payloadBytes, err = io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("Failed to read request body: %v", err),
				})
				fmt.Printf("❌ Failed to read request body: %v\n", err)
				return
			}
			fmt.Println("📦 Raw JSON Payload Dump:")
			fmt.Println(string(payloadBytes))

		case "multipart/form-data":
			if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("Failed to parse multipart form: %v", err),
				})
				fmt.Printf("❌ Failed to parse multipart form: %v\n", err)
				return
			}
			payloadField := c.Request.FormValue("payload")
			if payloadField == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "No 'payload' field found in multipart form",
				})
				fmt.Println("❌ No 'payload' field in multipart data")
				return
			}
			payloadBytes = []byte(payloadField)
			fmt.Println("📦 Multipart Payload Dump (payload field):")
			fmt.Println(payloadField)

		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Unsupported Content-Type",
			})
			fmt.Println("❌ Unsupported Content-Type")
			return
		}

		c.Request.Body = io.NopCloser(bytes.NewBuffer(payloadBytes))

		var payload map[string]interface{}
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("JSON Parsing Error: %v", err),
			})
			fmt.Printf("❌ JSON Parsing Error: %v\n", err)
			return
		}
		fmt.Println("✅ Webhook JSON parsed successfully.")

		// event
		eventType := ""
		if e, ok := payload["event"].(string); ok {
			eventType = e
		}

		// user
		accountTitle := ""
		if account, ok := payload["Account"].(map[string]interface{}); ok {
			if t, ok := account["title"].(string); ok {
				accountTitle = t
			}
		}

		// type => movie/episode, etc.
		mediaType := ""
		if md, ok := payload["Metadata"].(map[string]interface{}); ok {
			if t, ok := md["type"].(string); ok {
				mediaType = t
			}
		}

		fmt.Printf("🔹 Event: %s\n", eventType)
		fmt.Printf("🔹 User: %s\n", accountTitle)
		fmt.Printf("🔹 Media Type: %s\n", mediaType)

		/// HardCoded for now, Will implement structs to simplify logic, Making work for the time being.

		// 1) If user = jaken717 and event = media.pause => set rainbow
		if eventType == "media.pause" || eventType == "media.stop" && mediaType == "movie" && accountTitle == "jaken717" {
			fmt.Println("🎉 jaken717 paused => setting rainbow now...")
			if err := setPattern("rainbow", "0", ""); err != nil {
				fmt.Printf("❌ setPattern(rainbow) error: %v\n", err)
			} else {
				fmt.Println("✅ Rainbow set for jaken717 on media.pause.")
			}
		}

		// 2) If user = jaken717 and event=media.play and type=movie => chase pattern with purple, then off after 10s
		if eventType == "media.play" && mediaType == "movie" && accountTitle == "jaken717" {
			fmt.Println("🎬 jaken717 playing a movie => chase pattern (purple) then off in 10s...")
			if err := setPattern("chase", "5000", "128,0,128"); err != nil {
				fmt.Printf("❌ setPattern(chase,purple) error: %v\n", err)
			} else {
				fmt.Println("✅ Chase purple set for 10s.")
			}

			// after 10s => turn off
			time.AfterFunc(10*time.Second, func() {
				fmt.Println("⌛ 10s passed => turning off now for jaken717's movie.")
				if err := turnOff(); err != nil {
					fmt.Printf("❌ turnOff error after 10s: %v\n", err)
				} else {
					fmt.Println("✅ Lights turned off successfully after 10s for jaken717.")
				}
			})
		}
		if eventType == "media.resume" && mediaType == "movie" && accountTitle == "jaken717" {
			fmt.Println("🎉 jaken717 resume => killing lights now...")
			if err := turnOff(); err != nil {
				fmt.Printf("❌ turnOff error: %v\n", err)
			} else {
				fmt.Println("✅ Lights turned off after 10s total for TV episode.")
			}
		}

		// If event=media.play + type=episode => user-based color for 5s, then rainbow
		// TODO Not totally happy with this currently. Would like to set observability for only my users and not me.
		if (eventType == "media.play" || eventType == "media.resume") && mediaType == "episode" {
			var color string
			switch accountTitle {
			case "jaken717":
				color = "128,0,128" // Purple
			case "stephen1713":
				color = "255,255,0" // Yellow
			case "chelseasmi5":
				color = "0,255,0" // Green
			case "michaelschneider794":
				color = "255,0,0" // Red
			default:
				color = ""
			}

			if color != "" {
				fmt.Printf("📺 episode => user=%s => color=%s. Setting solid for 5s, then rainbow for 5s, then off.\n", accountTitle, color)

				// 1) Set user’s color (solid) for 5s
				if err := setPattern("chase", "5000", color); err != nil {
					fmt.Printf("❌ setPattern(solid) error: %v\n", err)
				} else {
					fmt.Println("✅ Solid color set for 5s.")
				}

				// After 5s → set rainbow for 5s
				time.AfterFunc(5*time.Second, func() {
					fmt.Println("⌛ 5s passed => setting rainbow for next 5s.")
					if err := setPattern("rainbow", "5000", ""); err != nil {
						fmt.Printf("❌ setPattern(rainbow) error: %v\n", err)
					} else {
						fmt.Println("✅ Rainbow set for 5s.")
					}
				})

			} else {
				fmt.Println("🔸 No matching user => no color set for TV episode.")
			}
		}

		// Done
		c.JSON(http.StatusOK, gin.H{"message": "Webhook processed successfully"})
	})

	fmt.Println("🎥 Webhook listener started on http://localhost:9091 🎥")
	if err := webhookRouter.Run(":9091"); err != nil {
		fmt.Printf("❌ Failed to start webhook server: %v\n", err)
	}
}
