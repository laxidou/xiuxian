/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { rpcStatus } from '../models/rpcStatus';
import type { v1AuthResponse } from '../models/v1AuthResponse';
import type { v1LoginRequest } from '../models/v1LoginRequest';
import type { v1RegisterRequest } from '../models/v1RegisterRequest';
import type { v1RotateMCPKeyRequest } from '../models/v1RotateMCPKeyRequest';
import type { v1RotateMCPKeyResponse } from '../models/v1RotateMCPKeyResponse';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class AuthServiceService {
    /**
     * @param expectedRoleId
     * @returns any A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static authServiceRevokeMcpKey(
        expectedRoleId?: string,
    ): CancelablePromise<any | rpcStatus> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/mcp-key',
            query: {
                'expectedRoleId': expectedRoleId,
            },
        });
    }
    /**
     * @param body
     * @returns v1RotateMCPKeyResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static authServiceRotateMcpKey(
        body: v1RotateMCPKeyRequest,
    ): CancelablePromise<v1RotateMCPKeyResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/mcp-key-rotations',
            body: body,
        });
    }
    /**
     * @param body
     * @returns v1AuthResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static authServiceRegister(
        body: v1RegisterRequest,
    ): CancelablePromise<v1AuthResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/registrations',
            body: body,
        });
    }
    /**
     * @returns any A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static authServiceLogout(): CancelablePromise<any | rpcStatus> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/session',
        });
    }
    /**
     * @param body
     * @returns v1AuthResponse A successful response.
     * @returns rpcStatus An unexpected error response.
     * @throws ApiError
     */
    public static authServiceLogin(
        body: v1LoginRequest,
    ): CancelablePromise<v1AuthResponse | rpcStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/sessions',
            body: body,
        });
    }
}
