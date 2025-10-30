package apiServer

import (
	"context"
	"net/http"
	"sync"

	"eveindustryplanner.com/esiworkers/apiServer/endpoints"
	"eveindustryplanner.com/esiworkers/apiServer/middleware"
	"eveindustryplanner.com/esiworkers/apiServer/util"
	"eveindustryplanner.com/esiworkers/logger"
)

type Server struct {
	Addr   string
	Router *http.ServeMux
}

func StartServer(wg *sync.WaitGroup) {
	server := &Server{
		Addr:   ":8080",
		Router: http.NewServeMux(),
	}
	server.routes()
	finalHandler := applyGlobalMiddleware(server.Router)
	server.Start(finalHandler)
}

func (s *Server) routes() {
	s.Router.HandleFunc("/auth", s.authHandler)
	s.Router.HandleFunc("/marketprices", endpoints.MarketPricesHandler())
	s.Router.HandleFunc("/systemindex", endpoints.SystemIndexesHandler())
}

// Handler for the /auth endpoint (POST)
func (s *Server) authHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simulating a fake authentication token response
	response := util.Response{Message: "Authentication successful", Token: "fake-jwt-token-12345"}
	util.JsonResponse(w, response)
}

func (s *Server) Start(h http.Handler) {
	log := logger.GetLogger(context.Background(), logger.ApiProvider)

	server := &http.Server{
		Addr:    s.Addr,
		Handler: h,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Error(err.Error())
	} else {
		log.Info("api service listening")
	}
}

func applyGlobalMiddleware(router *http.ServeMux) http.Handler {
	var handler http.Handler = router
	middlewares := []middleware.Middleware{
		middleware.LoggingMiddleware,
		middleware.RateLimitMiddleware,
	}

	for _, middleware := range middlewares {
		handler = middleware(handler)
	}

	return handler
}
