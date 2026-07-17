// Package main is the entry point for the Admin API server.
package main

import (
	"log"
	"net"
	"strings"

	"go_admin/config"
	"go_admin/database"
	"go_admin/middleware"
	"go_admin/routes"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize database
	if err := database.Init(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	log.Println("Database initialized successfully.")

	// Run auto migration
	if err := database.AutoMigrate(); err != nil {
		log.Fatalf("Failed to auto migrate: %v", err)
	}
	log.Println("Database migration completed.")

	r := gin.Default()

	// CORS middleware: allow localhost and private-LAN origins (dev/内网访问，
	// e.g. http://192.168.x.x:5174), so the admin panel works from any machine
	// on the LAN without maintaining an IP whitelist.
	r.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			host := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
			if i := strings.LastIndex(host, ":"); i >= 0 {
				host = host[:i]
			}
			if host == "localhost" || host == "127.0.0.1" {
				return true
			}
			// 内网穿透域名（与前端 vite allowedHosts 保持一致）
			if host == "lazyperson.top" || strings.HasSuffix(host, ".lazyperson.top") ||
				strings.HasSuffix(host, ".vicp.fun") {
				return true
			}
			ip := net.ParseIP(host)
			return ip != nil && ip.IsPrivate()
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// Public routes (no authentication required)
	routes.SetupPublicRoutes(r)

	// Protected routes (authentication required)
	protected := r.Group("/api/admin")
	protected.Use(middleware.JWTAuth())
	routes.SetupProtectedRoutes(protected)

	log.Printf("Admin Server starting on %s...\n", config.ServerAddr)
	if err := r.Run(config.ServerAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
