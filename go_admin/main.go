// Package main is the entry point for the Admin API server.
package main

import (
	"log"

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

	// CORS middleware
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5174", "http://localhost:3000"},
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
