export type CapturePhase = "request" | "response";

export interface TracePart {
  headers: Record<string, string | string[]>;
  body: string;
}

export interface CaptureEvent {
  id: string;
  requestId?: string;
  phase: CapturePhase;
  ts: string;
  method: string;
  url: string;
  host: string;
  status?: number;
  headers: Record<string, string | string[]>;
  body: string;
  bodyTruncated: boolean;
  source: string; // Preserve the source field of previously saved records.
}

export interface TraceEntry {
  id: string;
  requestId?: string;
  method: string;
  path: string;
  url: string;
  status: number;
  durationMs: number;
  timestamp: string;
  request: TracePart;
  response: TracePart;
  requestTruncated: boolean;
  responseTruncated: boolean;
}

export type TrafficRow = TraceEntry & {
  hasRequest: boolean;
  hasResponse: boolean;
};

export interface CaptureSession {
  id: string;
  createdAt: string;
  stoppedAt?: string;
}

export interface KeyJsonPayload {
  passwords: Record<string, string>;
  getPwdData: {
    密码: string;
    uit: string;
    den: number;
    pmn: number;
    pattern: string;
    whenLong: number;
    audio?: number;
    softwareName?: string;
    appMd5?: string;
  };
}
export interface KeyResult {
  sourceCount: number;
  updatedAt: string;
  valid: boolean;
  issues: string[];
  passwordCount: number;
  keyJson: KeyJsonPayload;
}

export interface KeyHistoryEntry {
  name: string;
  valid: boolean;
  issues: string[];
  id: string;
  createdAt: string;
  updatedAt: string;
  keyJson: KeyJsonPayload;
  // Older records contain only passwords; never substitute live requests.
  traces?: TrafficRow[];
  stoppedAt?: string;
}
