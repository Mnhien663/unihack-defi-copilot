
export interface SubmitHashChallengeRequest {
  session_id: string;
  review_hash: string;
  wallet_address: string;
  transaction_hash: string;
}

export interface ReceiptChallengeResponse {
  message: string;
  signature_expires_at: string;
}

export interface SwapReceiptResponse {
  pending_action_id: string;
  transaction_hash: string;
  status: string;
}

export async function requestSubmitHashChallenge(
  actionId: string,
  request: SubmitHashChallengeRequest
): Promise<ReceiptChallengeResponse> {
  if (!actionId.trim()) {
    throw new Error('Missing pending action ID.');
  }

  if (!request.session_id || request.session_id.length > 128) {
    throw new Error('Invalid session ID.');
  }

  if (!/^[0-9a-f]{64}$/.test(request.review_hash)) {
    throw new Error('Invalid review hash.');
  }

  if (!/^0x[0-9a-fA-F]{40}$/.test(request.wallet_address)) {
    throw new Error('Invalid wallet address.');
  }

  if (!/^0x[0-9a-fA-F]{64}$/.test(request.transaction_hash)) {
    throw new Error('Invalid transaction hash.');
  }

  const response = await fetch(
    `/api/v1/pending-actions/${encodeURIComponent(actionId)}/submit-hash/challenge`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    }
  );

  let data: {
    message?: string;
    signature_expires_at?: string;
    error?: { code?: string; message?: string };
  };

  try {
    data = await response.json();
  } catch {
    throw new Error('Backend returned invalid JSON.');
  }

  if (!response.ok) {
    throw new Error(
      `${data.error?.code ?? `HTTP_${response.status}`}: ${
        data.error?.message ?? 'Submit hash challenge failed.'
      }`
    );
  }

  if (
    typeof data.message !== 'string' ||
    !data.message ||
    typeof data.signature_expires_at !== 'string' ||
    !Number.isFinite(Date.parse(data.signature_expires_at))
  ) {
    throw new Error('Invalid receipt challenge response.');
  }

  return {
    message: data.message,
    signature_expires_at: data.signature_expires_at,
  };
}


export interface SignedSubmitHashRequest
  extends SubmitHashChallengeRequest {
  signature: string;
  signature_expires_at: string;
}

export async function submitTransactionHash(
  actionId: string,
  request: SignedSubmitHashRequest
): Promise<SwapReceiptResponse> {
  if (!actionId.trim()) {
    throw new Error('Missing pending action ID.');
  }

  if (!request.session_id || request.session_id.length > 128) {
    throw new Error('Invalid session ID.');
  }

  if (!/^[0-9a-f]{64}$/.test(request.review_hash)) {
    throw new Error('Invalid review hash.');
  }

  if (!/^0x[0-9a-fA-F]{40}$/.test(request.wallet_address)) {
    throw new Error('Invalid wallet address.');
  }

  if (!/^0x[0-9a-fA-F]{64}$/.test(request.transaction_hash)) {
    throw new Error('Invalid transaction hash.');
  }

  if (!/^0x[0-9a-fA-F]{130}$/.test(request.signature)) {
    throw new Error('Invalid wallet signature.');
  }

  const expiresAt = Date.parse(request.signature_expires_at);

  if (!Number.isFinite(expiresAt) || Date.now() >= expiresAt) {
    throw new Error('Receipt signature has expired.');
  }

  const response = await fetch(
    `/api/v1/pending-actions/${encodeURIComponent(actionId)}/submit-hash`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    }
  );

  let data: unknown;

  try {
    data = await response.json();
  } catch {
    throw new Error('Backend returned invalid JSON.');
  }

  if (!response.ok) {
    const errorResponse = data as {
      error?: { code?: string; message?: string };
    } | null;

    const code =
      errorResponse?.error?.code ?? `HTTP_${response.status}`;

    const message =
      errorResponse?.error?.message ??
      'Submit transaction hash failed.';

    throw new Error(`${code}: ${message}`);
  }

  if (
    !data ||
    typeof data !== 'object' ||
    !('pending_action_id' in data) ||
    typeof data.pending_action_id !== 'string' ||
    !('transaction_hash' in data) ||
    typeof data.transaction_hash !== 'string' ||
    !('status' in data) ||
    typeof data.status !== 'string'
    ) {
    throw new Error('Invalid swap receipt response.');
    }

    return data as SwapReceiptResponse;
}


export interface RefreshReceiptChallengeRequest {
  session_id: string;
  review_hash: string;
  wallet_address: string;
}

export async function requestRefreshReceiptChallenge(
  actionId: string,
  request: RefreshReceiptChallengeRequest
): Promise<ReceiptChallengeResponse> {
  if (!actionId.trim()) {
    throw new Error('Missing pending action ID.');
  }

  if (!request.session_id || request.session_id.length > 128) {
    throw new Error('Invalid session ID.');
  }

  if (!/^[0-9a-f]{64}$/.test(request.review_hash)) {
    throw new Error('Invalid review hash.');
  }

  if (!/^0x[0-9a-fA-F]{40}$/.test(request.wallet_address)) {
    throw new Error('Invalid wallet address.');
  }

  const response = await fetch(
    `/api/v1/pending-actions/${encodeURIComponent(actionId)}/refresh-receipt/challenge`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        session_id: request.session_id,
        review_hash: request.review_hash,
        wallet_address: request.wallet_address,
      }),
    }
  );

  let data: {
    message?: string;
    signature_expires_at?: string;
    error?: { code?: string; message?: string };
  };

  try {
    data = await response.json();
  } catch {
    throw new Error('Backend returned invalid JSON.');
  }

  if (!response.ok) {
    const code = data.error?.code ?? `HTTP_${response.status}`;
    const message =
      data.error?.message ?? 'Refresh receipt challenge failed.';

    throw new Error(`${code}: ${message}`);
  }

  if (
    typeof data.message !== 'string' ||
    !data.message ||
    typeof data.signature_expires_at !== 'string' ||
    !Number.isFinite(Date.parse(data.signature_expires_at))
  ) {
    throw new Error('Invalid refresh receipt challenge response.');
  }

  return {
    message: data.message,
    signature_expires_at: data.signature_expires_at,
  };
}


export interface SignedRefreshReceiptRequest
  extends RefreshReceiptChallengeRequest {
  signature: string;
  signature_expires_at: string;
}

export async function refreshSwapReceipt(
  actionId: string,
  request: SignedRefreshReceiptRequest
): Promise<SwapReceiptResponse> {
  if (!actionId.trim()) {
    throw new Error('Missing pending action ID.');
  }

  if (!request.session_id || request.session_id.length > 128) {
    throw new Error('Invalid session ID.');
  }

  if (!/^[0-9a-f]{64}$/.test(request.review_hash)) {
    throw new Error('Invalid review hash.');
  }

  if (!/^0x[0-9a-fA-F]{40}$/.test(request.wallet_address)) {
    throw new Error('Invalid wallet address.');
  }

  if (!/^0x[0-9a-fA-F]{130}$/.test(request.signature)) {
    throw new Error('Invalid wallet signature.');
  }

  const expiresAt = Date.parse(request.signature_expires_at);

  if (!Number.isFinite(expiresAt) || Date.now() >= expiresAt) {
    throw new Error('Receipt signature has expired.');
  }

  const response = await fetch(
    `/api/v1/pending-actions/${encodeURIComponent(actionId)}/refresh-receipt`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        session_id: request.session_id,
        review_hash: request.review_hash,
        wallet_address: request.wallet_address,
        signature: request.signature,
        signature_expires_at: request.signature_expires_at,
      }),
    }
  );

  let data: unknown;

  try {
    data = await response.json();
  } catch {
    throw new Error('Backend returned invalid JSON.');
  }

  if (!response.ok) {
    const errorResponse =
      data && typeof data === 'object'
        ? (data as {
            error?: { code?: string; message?: string };
          })
        : null;

    const code =
      errorResponse?.error?.code ?? `HTTP_${response.status}`;

    const message =
      errorResponse?.error?.message ??
      'Refresh receipt failed.';

    throw new Error(`${code}: ${message}`);
  }

  if (
    !data ||
    typeof data !== 'object' ||
    !('pending_action_id' in data) ||
    typeof data.pending_action_id !== 'string' ||
    !('transaction_hash' in data) ||
    typeof data.transaction_hash !== 'string' ||
    !('status' in data) ||
    typeof data.status !== 'string'
  ) {
    throw new Error('Invalid swap receipt response.');
  }

  return data as SwapReceiptResponse;
}


type SignWalletMessage = (
  message: string
) => Promise<`0x${string}`>;

export async function executeSignedSubmitHash(
  actionId: string,
  request: SubmitHashChallengeRequest,
  signMessage: SignWalletMessage
): Promise<SwapReceiptResponse> {
  // 1. Yêu cầu Backend tạo challenge
  const challenge = await requestSubmitHashChallenge(
    actionId,
    request
  );

  // 2. Kiểm tra thời hạn chữ ký
  const expiresAt = Date.parse(
    challenge.signature_expires_at
  );

  if (
    !Number.isFinite(expiresAt) ||
    Date.now() >= expiresAt
  ) {
    throw new Error(
      'Submit hash challenge has expired.'
    );
  }

  // 3. Yêu cầu người dùng ký message bằng ví
  const signature = await signMessage(challenge.message);

  // 4. Kiểm tra lại thời hạn sau khi người dùng ký
  if (Date.now() >= expiresAt) {
    throw new Error(
      'Signature expired while waiting for wallet.'
    );
  }

  // 5. Gửi chữ ký và transaction hash về Backend
  return submitTransactionHash(actionId, {
    ...request,
    signature,
    signature_expires_at:
      challenge.signature_expires_at,
  });
}


export async function executeSignedRefreshReceipt(
  actionId: string,
  request: RefreshReceiptChallengeRequest,
  signMessage: SignWalletMessage
): Promise<SwapReceiptResponse> {
  // 1. Backend tạo challenge cho Refresh Receipt
  const challenge = await requestRefreshReceiptChallenge(
    actionId,
    request
  );

  // 2. Kiểm tra thời hạn chữ ký
  const expiresAt = Date.parse(
    challenge.signature_expires_at
  );

  if (
    !Number.isFinite(expiresAt) ||
    Date.now() >= expiresAt
  ) {
    throw new Error(
      'Refresh receipt challenge has expired.'
    );
  }

  // 3. Người dùng ký message bằng Core Wallet
  const signature = await signMessage(challenge.message);

  // 4. Kiểm tra lại thời hạn sau khi ký
  if (Date.now() >= expiresAt) {
    throw new Error(
      'Signature expired while waiting for wallet.'
    );
  }

  // 5. Gửi chữ ký để Backend kiểm tra receipt
  return refreshSwapReceipt(actionId, {
    ...request,
    signature,
    signature_expires_at:
      challenge.signature_expires_at,
  });
}
