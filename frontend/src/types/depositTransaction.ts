import { decodeFunctionData, parseAbi } from 'viem';
import { canonicalDepositJson } from '../utils/canonicalDepositJson';

// Contract and asset addresses verified against backend/internal/avalanche/
// {tx.go,vault.go,deposit_tx.go} in the provided 2026-11-21 source snapshot.
const FUJI_CHAIN_ID = 43113;
const FUJI_MANAGER = '0xA7c9881eb43902Ca668bb864668eAA3795f2283B';
const DEMO_VAULT = '0x896C05deae3d6e35021B12DD361401736Bb8aAda';
const DEMO_ASSET = '0xcCCb83Fbe349fAebDE74Ce633D25B282E6FDd21A';
const ADDRESS_RE = /^0x[0-9a-fA-F]{40}$/;
const HEX_DATA_RE = /^0x(?:[0-9a-fA-F]{2})+$/;
const RAW_RE = /^(0|[1-9][0-9]*)$/;
const MAX_UINT256 = (1n << 256n) - 1n;

const depositAbi = parseAbi([
  'function executeDeposit(address asset, uint256 assets, uint256 minShares, uint256 deadline) returns (uint256 sharesOut)',
]);

type JsonObject = Record<string, unknown>;
const isRecord = (value: unknown): value is JsonObject =>
  Boolean(value && typeof value === 'object' && !Array.isArray(value));

function record(value: unknown, label: string): JsonObject {
  if (!isRecord(value)) throw new Error(`Missing ${label}.`);
  return value;
}

function address(value: unknown, label: string): string {
  if (typeof value !== 'string' || !ADDRESS_RE.test(value)) {
    throw new Error(`Invalid ${label}.`);
  }
  return value;
}

function expectAddress(actual: unknown, expected: string, label: string): void {
  if (address(actual, label).toLowerCase() !== expected.toLowerCase()) {
    throw new Error(`${label} does not match the reviewed deposit.`);
  }
}

function raw(value: unknown, label: string, positive = false): bigint {
  if (typeof value !== 'string' || !RAW_RE.test(value)) {
    throw new Error(`${label} must be a raw decimal string.`);
  }
  const parsed = BigInt(value);
  if (parsed > MAX_UINT256 || (positive && parsed <= 0n)) {
    throw new Error(`Invalid ${label} amount.`);
  }
  return parsed;
}

function unixSeconds(value: unknown, label: string): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`Invalid ${label}.`);
  }
  return value;
}

function requireFuture(ms: number, label: string): void {
  if (!Number.isFinite(ms) || Date.now() >= ms) {
    throw new Error(`${label} expired. Prepare a fresh deposit.`);
  }
}

export type DepositSendRequest = {
  to: `0x${string}`;
  data: `0x${string}`;
  value: bigint;
};

// Validate both the original pending-action transaction and the confirmed one.
// Do NOT construct the calldata independently, or send the transaction to the vault.
export function buildReviewedDepositSendRequest(
  confirmedArguments: unknown,
  reviewedArguments: unknown,
  connectedWallet: string,
  pendingExpiry: string,
  confirmedExpiry: string,
): DepositSendRequest {
  const original = record(reviewedArguments, 'reviewed arguments');
  const approved = record(confirmedArguments, 'confirmed arguments');
  const reviewedSummary = record(original.summary, 'reviewed deposit summary');
  const confirmedSummary = record(approved.summary, 'confirmed deposit summary');

  // Compare complete JSON content independent of object key order.
  if (canonicalDepositJson(reviewedSummary) !== canonicalDepositJson(confirmedSummary)) {
    throw new Error('Backend changed the deposit summary after review.');
  }
  const reviewedTx = record(original.transaction, 'reviewed transaction');
  const tx = record(approved.transaction, 'confirmed transaction');
  for (const key of ['chain_id', 'from', 'to', 'value', 'data']) {
    if (reviewedTx[key] !== tx[key]) {
      throw new Error(`Backend changed the reviewed deposit transaction (${key}).`);
    }
  }

  if (tx.chain_id !== FUJI_CHAIN_ID) {
    throw new Error('Deposit must use Avalanche Fuji (43113).');
  }
  expectAddress(tx.from, connectedWallet, 'Transaction sender');
  expectAddress(tx.to, FUJI_MANAGER, 'Execution Manager');
  expectAddress(reviewedSummary.owner_address, connectedWallet, 'Deposit owner');
  expectAddress(reviewedSummary.recipient_address, connectedWallet, 'Deposit recipient');
  expectAddress(reviewedSummary.manager_address, FUJI_MANAGER, 'Summary manager');
  expectAddress(reviewedSummary.vault_address, DEMO_VAULT, 'DemoVault');

  if (typeof tx.value !== 'string' || raw(tx.value, 'native value') !== 0n) {
    throw new Error('Deposit cannot send native AVAX as transaction value.');
  }
  if (typeof tx.data !== 'string' || !HEX_DATA_RE.test(tx.data)) {
    throw new Error('Invalid deposit calldata.');
  }

  const assetAmount = record(reviewedSummary.assets, 'assets');
  const asset = record(assetAmount.asset, 'asset token');
  const preview = record(reviewedSummary.preview_shares, 'preview shares');
  const minimum = record(reviewedSummary.min_shares, 'minimum shares');
  expectAddress(asset.address, DEMO_ASSET, 'Deposit asset');
  expectAddress(record(preview.asset, 'share token').address, DEMO_VAULT, 'Preview share token');
  expectAddress(record(minimum.asset, 'minimum share token').address, DEMO_VAULT, 'Minimum share token');
  if (asset.chain_id !== FUJI_CHAIN_ID ||
      record(preview.asset, 'share token').chain_id !== FUJI_CHAIN_ID ||
      record(minimum.asset, 'minimum share token').chain_id !== FUJI_CHAIN_ID) {
    throw new Error('Deposit token network mismatch.');
  }
  const assetsRaw = raw(assetAmount.raw, 'assets', true);
  const previewRaw = raw(preview.raw, 'preview shares', true);
  const minSharesRaw = raw(minimum.raw, 'minimum shares', true);
  if (minSharesRaw > previewRaw) {
    throw new Error('Minimum shares exceed preview shares.');
  }

  const deadline = unixSeconds(reviewedSummary.deadline, 'deposit deadline');
  requireFuture(deadline * 1000, 'Deposit deadline');
  requireFuture(Date.parse(String(reviewedSummary.preview_expires_at ?? '')), 'Deposit preview');
  requireFuture(Date.parse(pendingExpiry), 'Pending action');
  requireFuture(Date.parse(confirmedExpiry), 'Confirmed action');

  let decoded: ReturnType<typeof decodeFunctionData>;
  try {
    decoded = decodeFunctionData({
      abi: depositAbi,
      data: tx.data as `0x${string}`,
    });
  } catch {
    throw new Error('Calldata is not an executeDeposit transaction.');
  }
  if (decoded.functionName !== 'executeDeposit' || !decoded.args) {
    throw new Error('Unexpected Deposit contract function.');
  }
  const [decodedAsset, decodedAssets, decodedMinimum, decodedDeadline] = decoded.args;
  if (String(decodedAsset).toLowerCase() !== DEMO_ASSET.toLowerCase() ||
      decodedAssets !== assetsRaw ||
      decodedMinimum !== minSharesRaw ||
      decodedDeadline !== BigInt(deadline)) {
    throw new Error('Deposit calldata does not match reviewed values.');
  }

  return {
    to: tx.to as `0x${string}`,
    data: tx.data as `0x${string}`,
    value: 0n,
  };
}
