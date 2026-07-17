/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { rpcStatus } from '../models/rpcStatus';
import type { v1CloseConversationRequest } from '../models/v1CloseConversationRequest';
import type { v1Conversation } from '../models/v1Conversation';
import type { v1ConversationMessage } from '../models/v1ConversationMessage';
import type { v1ListConversationsResponse } from '../models/v1ListConversationsResponse';
import type { v1ListRecentEventsResponse } from '../models/v1ListRecentEventsResponse';
import type { v1MoveRequest } from '../models/v1MoveRequest';
import type { v1ReincarnateRequest } from '../models/v1ReincarnateRequest';
import type { v1RequestConversationRequest } from '../models/v1RequestConversationRequest';
import type { v1RespondConversationRequest } from '../models/v1RespondConversationRequest';
import type { v1RoleState } from '../models/v1RoleState';
import type { v1ScanRequest } from '../models/v1ScanRequest';
import type { v1ScanResponse } from '../models/v1ScanResponse';
import type { v1SeizeCultivationRequest } from '../models/v1SeizeCultivationRequest';
import type { v1SendConversationMessageRequest } from '../models/v1SendConversationMessageRequest';
import type { v1StopRequest } from '../models/v1StopRequest';
import type { v1TransferCultivationRequest } from '../models/v1TransferCultivationRequest';
import type { v1WorldBounds } from '../models/v1WorldBounds';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class WorldServiceService {
    /**
     * @param body
     * @returns v1Conversation A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceCloseConversation(
        body: v1CloseConversationRequest,
    ): CancelablePromise<v1Conversation | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/conversation-closures',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1ConversationMessage A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceSendConversationMessage(
        body: v1SendConversationMessageRequest,
    ): CancelablePromise<v1ConversationMessage | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/conversation-messages',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1Conversation A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceRespondConversation(
        body: v1RespondConversationRequest,
    ): CancelablePromise<v1Conversation | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/conversation-responses',
            body: body,
        });
    }
    /**
     * @returns v1ListConversationsResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceListConversations(): CancelablePromise<v1ListConversationsResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/conversations',
        });
    }
    /**
     * @param body
     * @returns v1Conversation A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceRequestConversation(
        body: v1RequestConversationRequest,
    ): CancelablePromise<v1Conversation | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/conversations',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1RoleState A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceSeizeCultivation(
        body: v1SeizeCultivationRequest,
    ): CancelablePromise<v1RoleState | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/cultivation-seizures',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1RoleState A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceTransferCultivation(
        body: v1TransferCultivationRequest,
    ): CancelablePromise<v1RoleState | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/cultivation-transfers',
            body: body,
        });
    }
    /**
     * @param after
     * @param limit
     * @returns v1ListRecentEventsResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceListRecentEvents(
        after?: string,
        limit?: number,
    ): CancelablePromise<v1ListRecentEventsResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/events',
            query: {
                'after': after,
                'limit': limit,
            },
        });
    }
    /**
     * @param body
     * @returns v1RoleState A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceStop(
        body: v1StopRequest,
    ): CancelablePromise<v1RoleState | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/movement-stops',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1RoleState A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceMove(
        body: v1MoveRequest,
    ): CancelablePromise<v1RoleState | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/movements',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1RoleState A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceReincarnate(
        body: v1ReincarnateRequest,
    ): CancelablePromise<v1RoleState | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/reincarnations',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1ScanResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceScan(
        body: v1ScanRequest,
    ): CancelablePromise<v1ScanResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/scans',
            body: body,
        });
    }
    /**
     * @returns v1RoleState A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceGetState(): CancelablePromise<v1RoleState | rpcStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/state',
        });
    }
    /**
     * @returns v1WorldBounds A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceGetWorldBounds(): CancelablePromise<v1WorldBounds | rpcStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/world/bounds',
        });
    }
}
