/**
 * Message type constants for the IPC protocol.
 * Organized by category with a prefix byte.
 */

// Instance lifecycle (0x01xx)
export const MsgInstanceNew = 0x0100;
export const MsgInstanceClose = 0x0101;
export const MsgInstanceWait = 0x0102;
export const MsgInstanceID = 0x0103;
export const MsgInstanceIsRunning = 0x0104;
export const MsgInstanceSetConsole = 0x0105;
export const MsgInstanceSetNetwork = 0x0106;
export const MsgInstanceExec = 0x0107;

// Filesystem operations (0x02xx)
export const MsgFsOpen = 0x0200;
export const MsgFsCreate = 0x0201;
export const MsgFsOpenFile = 0x0202;
export const MsgFsReadFile = 0x0203;
export const MsgFsWriteFile = 0x0204;
export const MsgFsStat = 0x0205;
export const MsgFsLstat = 0x0206;
export const MsgFsRemove = 0x0207;
export const MsgFsRemoveAll = 0x0208;
export const MsgFsMkdir = 0x0209;
export const MsgFsMkdirAll = 0x020a;
export const MsgFsRename = 0x020b;
export const MsgFsSymlink = 0x020c;
export const MsgFsReadlink = 0x020d;
export const MsgFsReadDir = 0x020e;
export const MsgFsChmod = 0x020f;
export const MsgFsChown = 0x0210;
export const MsgFsChtimes = 0x0211;
export const MsgFsSnapshot = 0x0212;

// File operations (0x03xx)
export const MsgFileClose = 0x0300;
export const MsgFileRead = 0x0301;
export const MsgFileWrite = 0x0302;
export const MsgFileSeek = 0x0303;
export const MsgFileSync = 0x0304;
export const MsgFileTruncate = 0x0305;
export const MsgFileStat = 0x0306;
export const MsgFileName = 0x0307;

// Command operations (0x04xx)
export const MsgCmdNew = 0x0400;
export const MsgCmdEntrypoint = 0x0401;
export const MsgCmdFree = 0x0402;
export const MsgCmdSetDir = 0x0403;
export const MsgCmdSetEnv = 0x0404;
export const MsgCmdGetEnv = 0x0405;
export const MsgCmdEnviron = 0x0406;
export const MsgCmdStart = 0x0407;
export const MsgCmdWait = 0x0408;
export const MsgCmdRun = 0x0409;
export const MsgCmdOutput = 0x040a;
export const MsgCmdCombinedOutput = 0x040b;
export const MsgCmdExitCode = 0x040c;
export const MsgCmdKill = 0x040d;

// Network operations (0x05xx)
export const MsgNetListen = 0x0500;
export const MsgListenerAccept = 0x0501;
export const MsgListenerClose = 0x0502;
export const MsgListenerAddr = 0x0503;
export const MsgConnRead = 0x0504;
export const MsgConnWrite = 0x0505;
export const MsgConnClose = 0x0506;
export const MsgConnLocalAddr = 0x0507;
export const MsgConnRemoteAddr = 0x0508;

// Snapshot operations (0x06xx)
export const MsgSnapshotCacheKey = 0x0600;
export const MsgSnapshotParent = 0x0601;
export const MsgSnapshotClose = 0x0602;
export const MsgSnapshotAsSource = 0x0603;

// Response types (0xFFxx)
export const MsgResponse = 0xff00;
export const MsgError = 0xff01;
