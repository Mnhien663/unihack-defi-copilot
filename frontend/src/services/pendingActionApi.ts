
export type PendingDecision = 'confirm' | 'reject';

export interface DecisionChallengeRequest {
  session_id: string;
  review_hash: string;
  decision: PendingDecision;
}

export interface DecisionChallengeResponse {
  message: string;
  expires_at: string;
}

interface BackendResponse {
  message?: string;
  expires_at?: string;
  error?: {
    code?: string;
    message?: string;
  };
}

export async function requestDecisionChallenge(
  actionId: string,
  request: DecisionChallengeRequest
): Promise<DecisionChallengeResponse> {
  // Backend yêu cầu review_hash gồm 64 ký tự hex viết thường
  if (!/^[0-9a-f]{64}$/.test(request.review_hash)) {
    throw new Error('Invalid review hash.');
  }

  const response = await fetch(
    `/api/v1/pending-actions/${encodeURIComponent(actionId)}/challenge`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(request),
    }
  );

  let data: BackendResponse;

  try {
    data = (await response.json()) as BackendResponse;
  } catch {
    throw new Error('Backend returned invalid JSON.');
  }

  if (!response.ok) {
    const code = data.error?.code ?? `HTTP_${response.status}`;
    const message =
      data.error?.message ?? 'Cannot create decision challenge.';

    throw new Error(`${code}: ${message}`);
  }

  if (
    typeof data.message !== 'string' ||
    !data.message ||
    typeof data.expires_at !== 'string'
  ) {
    throw new Error('Invalid challenge response.');
  }

  return {
    message: data.message,
    expires_at: data.expires_at,
  };
}


export interface DecisionSubmitRequest {
  session_id: string;
  review_hash: string;
  signature: string;
}

export type DecisionSubmitResponse =
  | {
      pending_action_id: string;
      status: 'rejected';
    }
  | {
      pending_action_id: string;
      status: 'confirmed';
      expires_at: string;
      arguments: unknown;
    };

export async function submitPendingDecision(
  actionId: string,
  decision: PendingDecision,
  request: DecisionSubmitRequest
): Promise<DecisionSubmitResponse> {
  if (!/^[0-9a-f]{64}$/.test(request.review_hash)) {
    throw new Error('Invalid review hash.');
  }

  if (!/^0x[0-9a-fA-F]{130}$/.test(request.signature)) {
    throw new Error('Invalid wallet signature.');
  }

  const response = await fetch(
    `/api/v1/pending-actions/${encodeURIComponent(actionId)}/${decision}`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(request),
    }
  );

  let data: unknown;

  try {
    data = await response.json();
  } catch {
    throw new Error('Backend returned invalid JSON.');
  }

  if (!data || typeof data !== 'object') {
    throw new Error('Invalid backend response.');
  }

  const result = data as {
    pending_action_id?: string;
    status?: string;
    expires_at?: string;
    arguments?: unknown;
    error?: {
      code?: string;
      message?: string;
    };
  };

  if (!response.ok) {
    const code = result.error?.code ?? `HTTP_${response.status}`;
    const message =
      result.error?.message ?? 'Decision request failed.';

    throw new Error(`${code}: ${message}`);
  }

  if (typeof result.pending_action_id !== 'string') {
    throw new Error('Missing pending action ID.');
  }

  if (decision === 'reject' && result.status === 'rejected') {
    return {
      pending_action_id: result.pending_action_id,
      status: 'rejected',
    };
  }

  if (
    decision === 'confirm' &&
    result.status === 'confirmed' &&
    typeof result.expires_at === 'string' &&
    result.arguments != null
  ) {
    return {
      pending_action_id: result.pending_action_id,
      status: 'confirmed',
      expires_at: result.expires_at,
      arguments: result.arguments,
    };
  }

  throw new Error('Unexpected decision response.');
}


type SignWalletMessage = (
  message: string
) => Promise<`0x${string}`>;

export async function signDecisionChallenge(
  actionId: string,
  request: DecisionChallengeRequest,
  signMessage: SignWalletMessage
): Promise<string> {
  // 1. Lấy message từ Backend
  const challenge = await requestDecisionChallenge(
    actionId,
    request
  );

  // 2. Kiểm tra challenge còn hiệu lực
  const expiresAt = Date.parse(challenge.expires_at);

  if (
    !Number.isFinite(expiresAt) ||
    Date.now() >= expiresAt
  ) {
    throw new Error(
      'Challenge expired. Please prepare a new swap.'
    );
  }

  // 3. Yêu cầu ví ký NGUYÊN VĂN message của Backend
  const signature = await signMessage(challenge.message);

  // 4. Trả chữ ký cho bước Confirm/Reject
  return signature;
}


export async function executeSignedDecision(
  actionId: string,
  sessionId: string,
  reviewHash: string,
  decision: PendingDecision,
  signMessage: SignWalletMessage
): Promise<DecisionSubmitResponse> {
  // 1. Kiểm tra dữ liệu trước khi yêu cầu ký
  if (!actionId.trim() || !sessionId.trim()) {
    throw new Error('Missing action ID or session ID.');
  }

  if (!/^[0-9a-f]{64}$/.test(reviewHash)) {
    throw new Error('Invalid review hash.');
  }

  // 2. Yêu cầu Backend tạo challenge và Core ký message
  const signature = await signDecisionChallenge(
    actionId,
    {
      session_id: sessionId,
      review_hash: reviewHash,
      decision,
    },
    signMessage
  );

  // 3. Gửi chữ ký về Backend để Confirm hoặc Reject
  return submitPendingDecision(actionId, decision, {
    session_id: sessionId,
    review_hash: reviewHash,
    signature,
  });
}