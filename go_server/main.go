// Package main is the entry point for the Go Gin API server.
package main

import (
	"log"

	"go_server/config"
	"go_server/database"
	"go_server/handlers"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize database
	if err := database.Init(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	log.Println("Database initialized successfully.")

	r := gin.Default()

	// CORS middleware
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// Static files for football team logos
	r.Static("/footballimg", config.FootballImgDir)

	// Static files for images
	r.Static("/images", "../public/images")

	// API routes
	api := r.Group("/api")
	{
		// Match routes
		api.GET("/matches", handlers.GetMatches)
		api.GET("/match/:id", handlers.GetMatchDetail)
		api.GET("/match/:id/history", handlers.GetMatchHistory)
		api.GET("/match/:id/odds/euro", handlers.GetMatchOddsEuro)
		api.GET("/match/:id/odds/pankou", handlers.GetMatchOddsPankou)
		api.GET("/analysis/matches", handlers.GetAnalysisMatches)
		api.GET("/analysis/rule-snapshot", handlers.GetAnalysisRuleSnapshot)
		api.POST("/analysis/rule-snapshot/generate", handlers.GenerateAnalysisRuleSnapshot)
		api.GET("/analysis/match/:id", handlers.GetAnalysisDetail)

		// Expert routes
		api.GET("/experts", handlers.GetExperts)
		api.GET("/match/:id/expert-tips", handlers.GetMatchExpertTips)

		// Leaderboard routes
		api.GET("/leaderboard", handlers.GetLeaderboard)

		// Prediction routes
		api.GET("/predictions", handlers.GetUserPredictions)
		api.GET("/predictions/stats", handlers.GetUserStats)
		api.POST("/predictions", handlers.CreatePrediction)

		// User routes
		api.GET("/user", handlers.GetCurrentUser)
		api.GET("/user/followed-teams", handlers.GetFollowedTeams)

		// News & transfer routes
		api.GET("/news", handlers.GetNews)
		api.GET("/transfers", handlers.GetTransferRumors)

		// Notification routes
		api.GET("/notifications", handlers.GetNotifications)
		api.GET("/notifications/unread-count", handlers.GetUnreadCount)
		api.PUT("/notifications/:id/read", handlers.MarkNotificationRead)
		api.PUT("/notifications/read-all", handlers.MarkAllNotificationsRead)

		// Wallet routes
		api.GET("/wallet/balance", handlers.GetWalletBalance)
		api.GET("/wallet/transactions", handlers.GetWalletTransactions)
		api.GET("/wallet/rewards", handlers.GetRewards)

		// League standings routes
		api.GET("/league/standings", handlers.GetLeagueStandings)
		api.GET("/league/top-scorers", handlers.GetTopScorers)

		// Search routes
		api.GET("/search", handlers.Search)
		api.GET("/search/history", handlers.GetSearchHistory)
		api.POST("/search/history", handlers.SaveSearchHistory)
		api.DELETE("/search/history", handlers.ClearSearchHistory)
	}

	log.Printf("Server starting on %s...\n", config.ServerAddr)
	if err := r.Run(config.ServerAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
