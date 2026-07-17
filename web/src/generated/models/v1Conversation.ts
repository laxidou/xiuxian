/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { v1ConversationMessage } from './v1ConversationMessage';
export type v1Conversation = {
    id?: string;
    requesterId?: string;
    recipientId?: string;
    status?: string;
    messages?: Array<v1ConversationMessage>;
    createdAtUnixMillis?: string;
    updatedAtUnixMillis?: string;
};

