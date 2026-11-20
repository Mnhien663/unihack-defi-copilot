import { decodeFunctionData, parseAbi } from 'viem';

export interface UnsignedSwapTransaction {
  chain_id: number;
  from: string;
  to: string;
  value: string;
  data: string;
}

export interface ConfirmedSwapArguments {
  transaction: UnsignedSwapTransaction;
}

const FUJI_CHAIN_ID = 43113;

const FUJI_MANAGER_ADDRESS =
  '0xA7c9881eb43902Ca668bb864668eAA3795f2283B';

export function validateUnsignedSwapTransaction(
  raw: unknown,
  connectedWallet: string
): UnsignedSwapTransaction {
  if (!raw || typeof raw !== 'object') {
    throw new Error('Invalid transaction data.');
  }

  const tx = raw as Record<string, unknown>;

  // 1. Kiểm tra đúng mạng Avalanche Fuji
  if (tx.chain_id !== FUJI_CHAIN_ID) {
    throw new Error('Transaction must use Avalanche Fuji.');
  }

  // 2. Kiểm tra địa chỉ ví gửi
  if (
    typeof tx.from !== 'string' ||
    !/^0x[0-9a-fA-F]{40}$/.test(tx.from) ||
    tx.from.toLowerCase() !== connectedWallet.toLowerCase()
  ) {
    throw new Error('Transaction sender does not match wallet.');
  }

  // 3. Kiểm tra hợp đồng nhận giao dịch
  if (
    typeof tx.to !== 'string' ||
    tx.to.toLowerCase() !== FUJI_MANAGER_ADDRESS.toLowerCase()
  ) {
    throw new Error('Unexpected transaction destination.');
  }

  // 4. Kiểm tra calldata (hex, đủ function selector)
  if (
    typeof tx.data !== 'string' ||
    !/^0x(?:[0-9a-fA-F]{2})+$/.test(tx.data) ||
    tx.data.length < 10
  ) {
    throw new Error('Invalid transaction calldata.');
  }

  decodeExecuteSwapCalldata(tx.data);

  // 5. Kiểm tra value là số nguyên không âm
  if (
    typeof tx.value !== 'string' ||
    !/^(?:[0-9]+|0x[0-9a-fA-F]+)$/.test(tx.value)
  ) {
    throw new Error('Invalid transaction value.');
  }
  
  // Swap token hiện tại không được gửi AVAX trực tiếp.
    if (BigInt(tx.value) !== 0n) {
        throw new Error(
            'Unexpected AVAX value in swap transaction.'
        );
    }

  return {
    chain_id: tx.chain_id,
    from: tx.from,
    to: tx.to,
    value: tx.value,
    data: tx.data,
  };
}

export interface SwapSendRequest {
  to: `0x${string}`;
  data: `0x${string}`;
  value: bigint;
}

export function buildSwapSendRequest(
  raw: unknown,
  connectedWallet: string
): SwapSendRequest {
  // Kiểm tra transaction trước khi chuyển đổi
  const tx = validateUnsignedSwapTransaction(
    raw,
    connectedWallet
  );

  return {
    to: tx.to as `0x${string}`,
    data: tx.data as `0x${string}`,
    value: BigInt(tx.value),
  };
}

const executeSwapAbi = parseAbi([
  'function executeSwap(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline) returns (uint256 amountOut)',
]);

export function decodeExecuteSwapCalldata(data: string) {
  try {
    const decoded = decodeFunctionData({
      abi: executeSwapAbi,
      data: data as `0x${string}`,
    });

    if (
      decoded.functionName !== 'executeSwap' ||
      !decoded.args
    ) {
      throw new Error('Unexpected contract function.');
    }

    const [
      tokenIn,
      tokenOut,
      amountIn,
      minAmountOut,
      deadline,
    ] = decoded.args;

    return {
      tokenIn,
      tokenOut,
      amountIn,
      minAmountOut,
      deadline,
    };
  } catch {
    throw new Error(
      'Transaction calldata is not a valid executeSwap call.'
    );
  }
}

export function validateSwapReviewParams(
  calldata: string,
  summary: {
    token_in: {
        address?: string;
        chain_id?: number;
    };
    token_out: {
        address?: string;
        chain_id?: number;
    };
    amount_in: { raw: string };
    min_amount_out: { raw: string };
    deadline: number;
    }
): void {
  const decoded = decodeExecuteSwapCalldata(calldata);

    const tokenInAddress = summary.token_in?.address;
    const tokenOutAddress = summary.token_out?.address;

    // Kiểm tra địa chỉ token có tồn tại và hợp lệ
    if (
    !tokenInAddress ||
    !tokenOutAddress ||
    !/^0x[0-9a-fA-F]{40}$/.test(tokenInAddress) ||
    !/^0x[0-9a-fA-F]{40}$/.test(tokenOutAddress)
    ) {
    throw new Error('Missing or invalid swap token address.');
    }

    // Kiểm tra cả hai token thuộc Avalanche Fuji
    if (
    summary.token_in.chain_id !== 43113 ||
    summary.token_out.chain_id !== 43113
    ) {
    throw new Error('Swap token network mismatch.');
    }

    // Đối chiếu địa chỉ token với calldata
    if (
    decoded.tokenIn.toLowerCase() !==
    tokenInAddress.toLowerCase()
    ) {
    throw new Error('Input token does not match swap review.');
    }

    if (
    decoded.tokenOut.toLowerCase() !==
    tokenOutAddress.toLowerCase()
    ) {
    throw new Error('Output token does not match swap review.');
    }

  // Kiểm tra số lượng token theo đơn vị nhỏ nhất
  if (
    !/^[0-9]+$/.test(summary.amount_in.raw) ||
    !/^[0-9]+$/.test(summary.min_amount_out.raw)
  ) {
    throw new Error('Invalid token amounts in swap review.');
  }

  if (
    decoded.amountIn !== BigInt(summary.amount_in.raw)
  ) {
    throw new Error('Swap input amount does not match review.');
  }

  if (
    decoded.minAmountOut !==
    BigInt(summary.min_amount_out.raw)
  ) {
    throw new Error('Minimum output does not match review.');
  }

  // Deadline là Unix timestamp tính bằng giây
  if (
    !Number.isSafeInteger(summary.deadline) ||
    summary.deadline <= 0 ||
    decoded.deadline !== BigInt(summary.deadline)
  ) {
    throw new Error('Swap deadline does not match review.');
  }

  if (decoded.deadline <= BigInt(Math.floor(Date.now() / 1000))) {
    throw new Error('Swap transaction deadline has expired.');
  }
}

export function buildReviewedSwapSendRequest(
  confirmedArguments: unknown,
  connectedWallet: string,
  reviewedSummary: Parameters<
    typeof validateSwapReviewParams
  >[1]
): SwapSendRequest {
  // 1. Kiểm tra response Confirm có transaction
  if (
    !confirmedArguments ||
    typeof confirmedArguments !== 'object' ||
    !('transaction' in confirmedArguments)
  ) {
    throw new Error(
      'Confirmed swap transaction is missing.'
    );
  }

  // 2. Kiểm tra transaction và chuyển sang kiểu Wagmi
  const sendRequest = buildSwapSendRequest(
    confirmedArguments.transaction,
    connectedWallet
  );

  // 3. Đối chiếu calldata với thông tin đã review
  validateSwapReviewParams(
    sendRequest.data,
    reviewedSummary
  );

  // 4. Chỉ trả về khi tất cả kiểm tra thành công
  return sendRequest;
}
