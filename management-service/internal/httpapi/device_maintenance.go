package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/deviceops"
)

type deviceMaintenanceRepository interface {
	ResolveDeviceMaintenanceIdentity(context.Context, string, string, string) (deviceops.Identity, error)
	CheckDeviceMaintenanceOwner(context.Context, deviceops.Identity, string) (deviceops.Identity, error)
	RecordDeviceDiagnosticBatch(context.Context, deviceops.StoredBatch, string, func() error) (deviceops.StoredBatch, bool, error)
	ListDeviceDiagnosticBatches(context.Context, int64, int64, int64, int) ([]deviceops.StoredBatch, error)
	GetDeviceDiagnosticBatch(context.Context, int64, int64, string, string) (deviceops.StoredBatch, error)
}
type deviceMaintenanceState struct {
	root, base, secret string
	signer             *deviceops.Signer
	store              deviceMaintenanceRepository
	limits             deviceBusinessLimiter
}

func (s *Server) InitDeviceMaintenance(mediaRoot string) error {
	signer, err := deviceops.NewSigner(s.xiaozhiInternalToken)
	if err != nil {
		return err
	}
	base, err := url.Parse(strings.TrimRight(s.publicWebURL, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && !(s.env == "test" && base.Scheme == "http")) {
		return errors.New("设备维护服务地址必须使用有效的安全连接")
	}
	if strings.TrimSpace(mediaRoot) == "" || s.store == nil {
		return errors.New("设备维护存储或数据服务尚未配置")
	}
	root, err := filepath.Abs(mediaRoot)
	if err != nil {
		return err
	}
	// A sibling of MEDIA_ROOT cannot be reached through any generic media alias.
	// This persistent directory is served only by the authorized handlers below.
	root = filepath.Clean(root) + "-device-private"
	if _, err := deviceops.PrivatePath(root, "device-firmware/catalog.json"); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return err
	}
	if err := deviceops.SyncPrivateDirectories(root, root); err != nil {
		return err
	}
	s.deviceMaintenance = &deviceMaintenanceState{root: root, base: strings.TrimRight(base.String(), "/"), secret: s.xiaozhiInternalToken, signer: signer, store: s.store}
	return nil
}
func (s *Server) registerDeviceMaintenanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/v1/xiaozhi/maintenance", s.deviceMaintenanceGrant)
	mux.HandleFunc("POST /internal/v1/xiaozhi/firmware-check", s.deviceFirmwareCheck)
	mux.HandleFunc("POST /api/v1/device-diagnostics/upload", s.deviceDiagnosticsUpload)
	mux.HandleFunc("GET /api/v1/devices/{deviceID}/diagnostics", s.deviceDiagnosticsList)
	mux.HandleFunc("GET /api/v1/devices/{deviceID}/diagnostics/{batchID}", s.deviceDiagnosticsDownload)
	mux.HandleFunc("GET /api/v1/device-firmware/{sha256}", s.deviceFirmwareDownload)
}
func (s *Server) maintenanceReady(w http.ResponseWriter) *deviceMaintenanceState {
	if s.deviceMaintenance == nil {
		writeError(w, 503, "设备维护服务尚未启用")
		return nil
	}
	return s.deviceMaintenance
}

type maintenanceInput struct {
	HardwareMAC   string            `json:"hardware_mac"`
	ConnectionID  string            `json:"connection_id,omitempty"`
	DeviceProfile deviceops.Profile `json:"device_profile"`
}

func maintenanceRead(r *http.Request, out any, limit int) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil {
		return errors.New("读取设备维护请求失败")
	}
	return deviceops.StrictJSON(raw, out, limit)
}
func (s *Server) deviceMaintenanceGrant(w http.ResponseWriter, r *http.Request) {
	s.maintenanceInternal(w, r, true)
}
func (s *Server) deviceFirmwareCheck(w http.ResponseWriter, r *http.Request) {
	s.maintenanceInternal(w, r, false)
}
func (s *Server) maintenanceInternal(w http.ResponseWriter, r *http.Request, withDiagnostics bool) {
	if !s.authorizeHardware(w, r) {
		return
	}
	state := s.maintenanceReady(w)
	if state == nil {
		return
	}
	var input maintenanceInput
	if err := maintenanceRead(r, &input, 4096); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := input.DeviceProfile.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if withDiagnostics && !deviceops.ValidConnectionID(input.ConnectionID) {
		writeError(w, 400, "设备连接标识无效")
		return
	}
	cid := ""
	if withDiagnostics {
		cid = input.ConnectionID
	}
	identity, err := state.store.ResolveDeviceMaintenanceIdentity(r.Context(), input.HardwareMAC, cid, state.secret)
	if err != nil {
		maintenanceError(w, err)
		return
	}
	now := time.Now().UTC()
	response := map[string]any{"firmware": nil}
	if withDiagnostics {
		token, err := state.signer.Issue(deviceops.Grant{Purpose: deviceops.PurposeDiagnostics, TenantID: identity.TenantID, DeviceID: identity.DeviceID, OwnerEpoch: identity.OwnerEpoch}, now)
		if err != nil {
			maintenanceError(w, err)
			return
		}
		response["diagnostics"] = map[string]any{"upload_url": state.base + "/api/v1/device-diagnostics/upload", "token": token, "owner_epoch": identity.OwnerEpoch, "expires_at": now.Add(deviceops.GrantTTL).Unix(), "max_batch_bytes": deviceops.MaxBatchBytes, "max_batch_events": deviceops.MaxBatchEvents}
	}
	// A broken/absent catalog must never prevent connection or log recovery.
	if offer, err := state.firmwareOffer(identity, input.DeviceProfile, now); err == nil && offer != nil {
		response["firmware"] = offer
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, response)
}
func (st *deviceMaintenanceState) firmwareOffer(identity deviceops.Identity, profile deviceops.Profile, now time.Time) (*deviceops.Offer, error) {
	if identity.Busy {
		return nil, nil
	}
	catalog, err := deviceops.ReadCatalog(st.root)
	if err != nil {
		return nil, err
	}
	entry, err := catalog.Select(profile, identity.DeviceID, "")
	if err != nil || entry == nil {
		return nil, err
	}
	if _, err := deviceops.ValidateFirmware(st.root, *entry); err != nil {
		return nil, err
	}
	token, err := st.signer.Issue(deviceops.Grant{Purpose: deviceops.PurposeFirmware, TenantID: identity.TenantID, DeviceID: identity.DeviceID, OwnerEpoch: identity.OwnerEpoch, ArtifactSHA256: entry.SHA256, Profile: &profile}, now)
	if err != nil {
		return nil, err
	}
	offer := entry.Offer(st.base + "/api/v1/device-firmware/" + entry.SHA256 + "?grant=" + url.QueryEscape(token))
	return &offer, nil
}
func maintenanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, deviceops.ErrForbidden), errors.Is(err, sql.ErrNoRows):
		writeError(w, 403, "设备归属或连接已变化，请重新连接后重试")
	case errors.Is(err, deviceops.ErrConflict):
		writeError(w, 409, deviceops.ErrConflict.Error())
	case errors.Is(err, deviceops.ErrQuota):
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, deviceops.ErrQuota.Error())
	default:
		writeError(w, 503, "设备维护暂时不可用，请稍后重试")
	}
}
func maintenanceBearer(r *http.Request) string {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
}
func (s *Server) deviceDiagnosticsUpload(w http.ResponseWriter, r *http.Request) {
	state := s.maintenanceReady(w)
	if state == nil {
		return
	}
	grant, err := state.signer.Verify(maintenanceBearer(r), deviceops.PurposeDiagnostics, time.Now().UTC())
	if err != nil {
		writeError(w, 401, "日志上传授权无效或已过期")
		return
	}
	if !state.limits.allow("diagnostics:"+strconv.FormatInt(grant.DeviceID, 10), 30) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "设备日志上传过于频繁，请稍后重试")
		return
	}
	if enc := strings.TrimSpace(r.Header.Get("Content-Encoding")); enc != "" && enc != "identity" {
		writeError(w, 400, "日志上传暂不接受压缩传输")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, deviceops.MaxBatchBytes+1))
	if err != nil {
		writeError(w, 400, "读取日志批次失败")
		return
	}
	batch, err := deviceops.DecodeBatch(raw)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if batch.OwnerEpoch != grant.OwnerEpoch {
		writeError(w, 403, "日志批次不属于当前设备归属，请重新连接")
		return
	}
	checksum := deviceops.BatchChecksum(raw)
	identity := grant.Identity()
	key := deviceops.BatchObjectKey(identity, batch, checksum)
	meta := deviceops.StoredBatch{TenantID: identity.TenantID, DeviceID: identity.DeviceID, OwnerEpoch: identity.OwnerEpoch, BatchID: batch.BatchID, BootID: batch.Events[0].BootID, FirmwareVersion: batch.FirmwareVersion, FirmwareSHA256: batch.FirmwareSHA256, Storage: batch.Storage, SeqStart: batch.Events[0].Seq, SeqEnd: batch.Events[len(batch.Events)-1].Seq, EventCount: len(batch.Events), SizeBytes: int64(len(raw)), ChecksumSHA256: checksum, ObjectKey: key}
	stored, duplicate, err := state.store.RecordDeviceDiagnosticBatch(r.Context(), meta, state.secret, func() error { return deviceops.SaveBatch(state.root, key, raw) })
	if err != nil {
		maintenanceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"accepted": true, "batch_id": stored.BatchID, "checksum_sha256": stored.ChecksumSHA256, "duplicate": duplicate})
}
func (s *Server) deviceDiagnosticsList(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	s.deviceDiagnosticsListForTenant(w, r, tenant)
}

// tenant is derived by deviceCustomer, never from a query/body/header value.
func (s *Server) deviceDiagnosticsListForTenant(w http.ResponseWriter, r *http.Request, tenant int64) {
	state := s.maintenanceReady(w)
	if state == nil {
		return
	}
	device, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	before := int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		var err error
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			writeError(w, 400, "日志分页位置无效")
			return
		}
	}
	items, err := state.store.ListDeviceDiagnosticBatches(r.Context(), tenant, device, before, 50)
	if err != nil {
		maintenanceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) deviceDiagnosticsDownload(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	s.deviceDiagnosticsDownloadForTenant(w, r, tenant)
}

func (s *Server) deviceDiagnosticsDownloadForTenant(w http.ResponseWriter, r *http.Request, tenant int64) {
	state := s.maintenanceReady(w)
	if state == nil {
		return
	}
	device, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	batch, err := state.store.GetDeviceDiagnosticBatch(r.Context(), tenant, device, r.URL.Query().Get("owner_epoch"), r.PathValue("batchID"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "日志批次不存在")
		} else {
			maintenanceError(w, err)
		}
		return
	}
	file, err := deviceops.PrivatePath(state.root, batch.ObjectKey)
	if err != nil {
		maintenanceError(w, err)
		return
	}
	f, err := os.Open(file)
	if err != nil {
		writeError(w, 404, "日志文件暂不可读取")
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, deviceops.MaxBatchBytes+1))
	if err != nil || int64(len(raw)) != batch.SizeBytes || deviceops.BatchChecksum(raw) != batch.ChecksumSHA256 {
		writeError(w, 503, "日志文件校验失败，请联系管理员")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"device-diagnostics.json\"")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(raw)
}
func (s *Server) deviceFirmwareDownload(w http.ResponseWriter, r *http.Request) {
	state := s.maintenanceReady(w)
	if state == nil {
		return
	}
	token := maintenanceBearer(r)
	if token == "" {
		token = r.URL.Query().Get("grant")
	}
	grant, err := state.signer.Verify(token, deviceops.PurposeFirmware, time.Now().UTC())
	if err != nil || grant.ArtifactSHA256 != r.PathValue("sha256") {
		writeError(w, 401, "固件下载授权无效或已过期")
		return
	}
	identity, err := state.store.CheckDeviceMaintenanceOwner(r.Context(), grant.Identity(), state.secret)
	if err != nil {
		maintenanceError(w, err)
		return
	}
	if identity.Busy {
		writeError(w, 409, "设备正在直播，请结束本场后再升级")
		return
	}
	catalog, err := deviceops.ReadCatalog(state.root)
	if err != nil {
		writeError(w, 503, "固件清单暂不可用")
		return
	}
	entry, err := catalog.Select(*grant.Profile, identity.DeviceID, grant.ArtifactSHA256)
	if err != nil || entry == nil {
		writeError(w, 404, "该固件当前未开放升级")
		return
	}
	path, err := deviceops.ValidateFirmware(state.root, *entry)
	if err != nil {
		writeError(w, 503, "固件校验失败，请稍后重试")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, 503, "固件暂不可读取")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, 503, "固件暂不可读取")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "application-"+entry.Version+".bin", info.ModTime(), file)
}
