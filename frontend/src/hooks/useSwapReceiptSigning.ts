
import { useConnection, useSignMessage } from 'wagmi';

import {
  executeSignedSubmitHash,
  executeSignedRefreshReceipt,
  type SubmitHashChallengeRequest,
  type RefreshReceiptChallengeRequest,
} from '../services/swapReceiptApi';

const FUJI_CHAIN_ID = 43113;

export function useSwapReceiptSigning(isMock = true) {
  const { address, isConnected, chainId } = useConnection();
  const signMessage = useSignMessage();

  function assertWalletReady(walletAddress: string) {
    if (isMock) {
      throw new Error('MOCK transactions cannot be signed.');
    }

    if (!isConnected || !address) {
      throw new Error('Please connect Core Wallet.');
    }

    if (chainId !== FUJI_CHAIN_ID) {
      throw new Error('Please switch to Avalanche Fuji.');
    }

    if (
      address.toLowerCase() !== walletAddress.toLowerCase()
    ) {
      throw new Error('Connected wallet does not match the action.');
    }

    return address;
  }

  async function submitHashWithWallet(
    actionId: string,
    request: SubmitHashChallengeRequest
  ) {
    const account = assertWalletReady(request.wallet_address);

    return executeSignedSubmitHash(
      actionId,
      request,
      (message) =>
        signMessage.mutateAsync({
          message,
          account,
        })
    );
  }

  async function refreshReceiptWithWallet(
    actionId: string,
    request: RefreshReceiptChallengeRequest
  ) {
    const account = assertWalletReady(request.wallet_address);

    return executeSignedRefreshReceipt(
      actionId,
      request,
      (message) =>
        signMessage.mutateAsync({
          message,
          account,
        })
    );
  }

  return {
    submitHashWithWallet,
    refreshReceiptWithWallet,
    isSigning: signMessage.isPending,
  };
}
