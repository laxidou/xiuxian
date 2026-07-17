/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { rpcStatus } from '../models/rpcStatus';
import type { v1CloseConversationRequest } from '../models/v1CloseConversationRequest';
import type { v1Conversation } from '../models/v1Conversation';
import type { v1ConversationMessage } from '../models/v1ConversationMessage';
import type { v1GetStateRequest } from '../models/v1GetStateRequest';
import type { v1GetWorldBoundsRequest } from '../models/v1GetWorldBoundsRequest';
import type { v1ListConversationsRequest } from '../models/v1ListConversationsRequest';
import type { v1ListConversationsResponse } from '../models/v1ListConversationsResponse';
import type { v1ListRecentEventsRequest } from '../models/v1ListRecentEventsRequest';
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
            url: '/xiuxian.v1.WorldService/CloseConversation',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1RoleState A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceGetState(
        body: v1GetStateRequest,
    ): CancelablePromise<v1RoleState | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/xiuxian.v1.WorldService/GetState',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1WorldBounds A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceGetWorldBounds(
        body: v1GetWorldBoundsRequest,
    ): CancelablePromise<v1WorldBounds | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/xiuxian.v1.WorldService/GetWorldBounds',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1ListConversationsResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceListConversations(
        body: v1ListConversationsRequest,
    ): CancelablePromise<v1ListConversationsResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/xiuxian.v1.WorldService/ListConversations',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1ListRecentEventsResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static worldServiceListRecentEvents(
        body: v1ListRecentEventsRequest,
    ): CancelablePromise<v1ListRecentEventsResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/xiuxian.v1.WorldService/ListRecentEvents',
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
            url: '/xiuxian.v1.WorldService/Move',
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
            url: '/xiuxian.v1.WorldService/Reincarnate',
            body: body,
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
            url: '/xiuxian.v1.WorldService/RequestConversation',
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
            url: '/xiuxian.v1.WorldService/RespondConversation',
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
            url: '/xiuxian.v1.WorldService/Scan',
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
            url: '/xiuxian.v1.WorldService/SeizeCultivation',
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
            url: '/xiuxian.v1.WorldService/SendConversationMessage',
            body: body,
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
            url: '/xiuxian.v1.WorldService/Stop',
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
            url: '/xiuxian.v1.WorldService/TransferCultivation',
            body: body,
        });
    }
}
