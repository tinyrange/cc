package vm

import sidecarproto "j5.nz/cc/internal/vm/sidecar"

const WorkerProtocolVersion = sidecarproto.WorkerProtocolVersion
const WorkerTLSScheme = sidecarproto.WorkerTLSScheme

const (
	WorkerServiceControl   = sidecarproto.WorkerServiceControl
	WorkerServiceExec      = sidecarproto.WorkerServiceExec
	WorkerServiceConsole   = sidecarproto.WorkerServiceConsole
	WorkerServiceVirtioFS  = sidecarproto.WorkerServiceVirtioFS
	WorkerServiceVirtioNet = sidecarproto.WorkerServiceVirtioNet
)

const (
	WorkerFrameHello        = sidecarproto.WorkerFrameHello
	WorkerFrameStart        = sidecarproto.WorkerFrameStart
	WorkerFrameStartBlank   = sidecarproto.WorkerFrameStartBlank
	WorkerFrameStop         = sidecarproto.WorkerFrameStop
	WorkerFrameWait         = sidecarproto.WorkerFrameWait
	WorkerFrameStatus       = sidecarproto.WorkerFrameStatus
	WorkerFrameExec         = sidecarproto.WorkerFrameExec
	WorkerFrameAddShare     = sidecarproto.WorkerFrameAddShare
	WorkerFrameExecInput    = sidecarproto.WorkerFrameExecInput
	WorkerFrameExecInputAck = sidecarproto.WorkerFrameExecInputAck
	WorkerFrameCancel       = sidecarproto.WorkerFrameCancel
	WorkerFrameFlush        = sidecarproto.WorkerFrameFlush
	WorkerFrameConsole      = sidecarproto.WorkerFrameConsole
	WorkerFrameDone         = sidecarproto.WorkerFrameDone
	WorkerFrameEvent        = sidecarproto.WorkerFrameEvent
	WorkerFramePacket       = sidecarproto.WorkerFramePacket
	WorkerFrameFilesystemOp = sidecarproto.WorkerFrameFilesystemOp
	WorkerFrameError        = sidecarproto.WorkerFrameError
)

type VMHostCapabilities = sidecarproto.HostCapabilities
type WorkerHello = sidecarproto.WorkerHello
type WorkerStartRequest = sidecarproto.WorkerStartRequest
type WorkerStartResponse = sidecarproto.WorkerStartResponse
type WorkerStatusRequest = sidecarproto.WorkerStatusRequest
type WorkerStatusResponse = sidecarproto.WorkerStatusResponse
type WorkerStopRequest = sidecarproto.WorkerStopRequest
type WorkerWaitRequest = sidecarproto.WorkerWaitRequest
type WorkerFlushRequest = sidecarproto.WorkerFlushRequest
type WorkerAddShareRequest = sidecarproto.WorkerAddShareRequest
type WorkerConsoleRequest = sidecarproto.WorkerConsoleRequest
type WorkerConsoleResponse = sidecarproto.WorkerConsoleResponse
type WorkerExecRequest = sidecarproto.WorkerExecRequest
type WorkerExecInput = sidecarproto.WorkerExecInput
type WorkerCancelRequest = sidecarproto.WorkerCancelRequest
type WorkerError = sidecarproto.WorkerError
type WorkerFrame = sidecarproto.WorkerFrame
type WorkerCodec = sidecarproto.WorkerCodec
type WorkerSecurityError = sidecarproto.WorkerSecurityError
type WorkerSecurityReason = sidecarproto.WorkerSecurityReason
type WorkerTransportSecurity = sidecarproto.WorkerTransportSecurity

const (
	WorkerSecurityPlaintextTCPRejected = sidecarproto.WorkerSecurityPlaintextTCPRejected
	WorkerSecurityTLSConfigRequired    = sidecarproto.WorkerSecurityTLSConfigRequired
	WorkerSecurityInvalidTLSConfig     = sidecarproto.WorkerSecurityInvalidTLSConfig
	WorkerSecurityPeerScopeMismatch    = sidecarproto.WorkerSecurityPeerScopeMismatch
	WorkerSecurityHandshakeFailed      = sidecarproto.WorkerSecurityHandshakeFailed
)

var NewWorkerFrame = sidecarproto.NewWorkerFrame
var NewWorkerCodec = sidecarproto.NewWorkerCodec
var LoadWorkerServerSecurity = sidecarproto.LoadWorkerServerSecurity
var HandshakeWorkerServer = sidecarproto.HandshakeWorkerServer
