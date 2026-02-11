package server

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/primal-host/avalauncher/internal/config"
	"github.com/primal-host/avalauncher/internal/manager"
)

func (s *Server) routes() {
	s.echo.GET("/health", s.handleHealth)
	s.echo.GET("/", s.handleDashboard)
	s.echo.GET("/api/status", s.handleStatus)

	// Authenticated API group.
	api := s.echo.Group("/api", s.requireBearer)
	api.POST("/nodes", s.handleCreateNode)
	api.GET("/nodes", s.handleListNodes)
	api.GET("/nodes/:id", s.handleGetNode)
	api.POST("/nodes/:id/start", s.handleStartNode)
	api.POST("/nodes/:id/stop", s.handleStopNode)
	api.DELETE("/nodes/:id", s.handleDeleteNode)
	api.GET("/nodes/:id/logs", s.handleNodeLogs)
	api.GET("/events", s.handleListEvents)
	api.GET("/hosts", s.handleListHosts)
	api.POST("/hosts", s.handleAddHost)
	api.DELETE("/hosts/:id", s.handleRemoveHost)
	api.POST("/l1s", s.handleCreateL1)
	api.GET("/l1s", s.handleListL1s)
	api.GET("/l1s/:id", s.handleGetL1)
	api.DELETE("/l1s/:id", s.handleDeleteL1)
	api.POST("/l1s/:id/validators", s.handleAddValidator)
	api.DELETE("/l1s/:id/validators/:nodeId", s.handleRemoveValidator)
}

// requireBearer is Echo middleware that checks the Authorization header.
func (s *Server) requireBearer(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !s.checkBearer(c) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		return next(c)
	}
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "ok",
		"version": config.Version,
	})
}

func (s *Server) handleDashboard(c echo.Context) error {
	html := strings.ReplaceAll(dashboardHTML, "{{VERSION}}", config.Version)
	return c.HTML(http.StatusOK, html)
}

func (s *Server) handleStatus(c echo.Context) error {
	authenticated := s.checkBearer(c)
	ctx := c.Request().Context()

	counts := map[string]int64{}
	tables := []string{"hosts", "nodes", "l1s", "events"}
	for _, t := range tables {
		var n int64
		// Table names are hardcoded constants, not user input.
		err := s.db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+t).Scan(&n)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		counts[t] = n
	}

	resp := map[string]any{
		"version": config.Version,
		"counts":  counts,
	}

	if authenticated {
		resp["authenticated"] = true
		nodes, err := s.mgr.ListNodes(ctx)
		if err == nil {
			hostLabels := s.mgr.HostLabelsMap(ctx)
			summaries := make([]manager.NodeSummary, 0, len(nodes))
			for _, n := range nodes {
				l1s, _ := s.mgr.ListL1sForNode(ctx, n.ID)
				if l1s == nil {
					l1s = []manager.L1Summary{}
				}
				hostName := hostLabels[n.HostID]
				if hostName == "" {
					hostName = "unknown"
				}
				summaries = append(summaries, manager.NodeSummary{
					ID:          n.ID,
					Name:        n.Name,
					HostName:    hostName,
					Image:       n.Image,
					NodeID:      n.NodeID,
					StakingPort: n.StakingPort,
					Status:      n.Status,
					L1s:         l1s,
				})
			}
			resp["nodes"] = summaries
		}

		hosts, err := s.mgr.ListHosts(ctx)
		if err == nil {
			resp["hosts_list"] = hosts
		}

		l1sList, err := s.mgr.ListL1sForDashboard(ctx)
		if err == nil {
			resp["l1s_list"] = l1sList
		}
	}

	return c.JSON(http.StatusOK, resp)
}

func (s *Server) handleCreateNode(c echo.Context) error {
	var req manager.CreateNodeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	node, err := s.mgr.CreateNode(c.Request().Context(), req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, node)
}

func (s *Server) handleListNodes(c echo.Context) error {
	nodes, err := s.mgr.ListNodes(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if nodes == nil {
		nodes = []manager.Node{}
	}
	return c.JSON(http.StatusOK, nodes)
}

func (s *Server) handleGetNode(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	node, err := s.mgr.GetNode(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "node not found"})
	}
	return c.JSON(http.StatusOK, node)
}

func (s *Server) handleStartNode(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	if err := s.mgr.StartNode(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "started"})
}

func (s *Server) handleStopNode(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	if err := s.mgr.StopNode(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) handleDeleteNode(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	removeVolumes := c.QueryParam("remove_volumes") == "true"
	if err := s.mgr.DeleteNode(c.Request().Context(), id, removeVolumes); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleNodeLogs(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	tail := c.QueryParam("tail")
	reader, err := s.mgr.NodeLogs(c.Request().Context(), id, tail)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	defer reader.Close()

	c.Response().Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Response().WriteHeader(http.StatusOK)
	io.Copy(c.Response().Writer, reader)
	return nil
}

func (s *Server) handleListEvents(c echo.Context) error {
	limit := 50
	if l := c.QueryParam("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	events, err := s.mgr.ListEvents(c.Request().Context(), limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if events == nil {
		events = []manager.Event{}
	}
	return c.JSON(http.StatusOK, events)
}

func (s *Server) handleListHosts(c echo.Context) error {
	hosts, err := s.mgr.ListHosts(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, hosts)
}

func (s *Server) handleAddHost(c echo.Context) error {
	var req manager.AddHostRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	host, err := s.mgr.AddHost(c.Request().Context(), req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, host)
}

func (s *Server) handleRemoveHost(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	if err := s.mgr.RemoveHost(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleCreateL1(c echo.Context) error {
	var req manager.CreateL1Request
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	l1, err := s.mgr.CreateL1(c.Request().Context(), req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, l1)
}

func (s *Server) handleListL1s(c echo.Context) error {
	l1s, err := s.mgr.ListL1s(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, l1s)
}

func (s *Server) handleGetL1(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	l1, err := s.mgr.GetL1(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "L1 not found"})
	}
	return c.JSON(http.StatusOK, l1)
}

func (s *Server) handleDeleteL1(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	if err := s.mgr.DeleteL1(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAddValidator(c echo.Context) error {
	l1ID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	var req manager.AddValidatorRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	val, err := s.mgr.AddValidator(c.Request().Context(), l1ID, req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, val)
}

func (s *Server) handleRemoveValidator(c echo.Context) error {
	l1ID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	nodeID, err := strconv.ParseInt(c.Param("nodeId"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid node id"})
	}
	if err := s.mgr.RemoveValidator(c.Request().Context(), l1ID, nodeID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) checkBearer(c echo.Context) bool {
	if s.adminKey == "" {
		return false
	}
	auth := c.Request().Header.Get("Authorization")
	return strings.TrimPrefix(auth, "Bearer ") == s.adminKey
}
