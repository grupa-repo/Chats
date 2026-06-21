SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;
SET default_tablespace = '';
SET default_table_access_method = heap;

-- Create chat schema for the chat microservice
CREATE SCHEMA IF NOT EXISTS chat;

CREATE EXTENSION IF NOT EXISTS "uuid-ossp" SCHEMA public; -- To use uuid_generate_v7 custom function.

-- Creating uuid generate v7 method - this feature is not implemented in postgres 17 yet.
-- Will be implemented in 18.
create or replace function public.uuid_generate_v7()
returns uuid
as $$
begin
  -- use random v4 uuid as starting point (which has the same variant we need)
  -- then overlay timestamp
  -- then set version 7 by flipping the 2 and 1 bit in the version 4 string
  return encode(
    set_bit(
      set_bit(
        overlay(uuid_send(gen_random_uuid())
                placing substring(int8send(floor(extract(epoch from clock_timestamp()) * 1000)::bigint) from 3)
                from 1 for 6
        ),
        52, 1
      ),
      53, 1
    ),
    'hex')::uuid;
end
$$
language plpgsql
volatile;

-- Messages table - stores the actual chat messages
CREATE TABLE IF NOT EXISTS chat.message (
    id UUID PRIMARY KEY DEFAULT public.uuid_generate_v7(),
    chat_id UUID NOT NULL,
    sender_id UUID NOT NULL,
    content TEXT NOT NULL,
    message_type VARCHAR(20) NOT NULL DEFAULT 'text' CHECK (message_type IN ('text', 'image', 'video', 'audio', 'file')),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    read_status BOOLEAN DEFAULT FALSE,
    deleted_at TIMESTAMP WITH TIME ZONE,
    deleted_by UUID
);

CREATE TABLE IF NOT EXISTS chat.chat (
    id uuid NOT NULL,
    type CHARACTER VARYING(20) NOT NULL CHECK (type IN ('private', 'group', 'container')),
    usergroup_id bigint,
    container_id uuid,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT pk_chat PRIMARY KEY (id)
);

INSERT INTO chat.chat(id, type, usergroup_id, container_id, created_at) VALUES ('01987073-0a87-7b32-9439-86868dfe9bd2', 'group', 1, NULL, CURRENT_TIMESTAMP);
INSERT INTO chat.chat(id, type, usergroup_id, container_id, created_at) VALUES ('01987073-cf13-7621-af36-54ce20056d18', 'group', 2, NULL, CURRENT_TIMESTAMP);
INSERT INTO chat.chat(id, type, usergroup_id, container_id, created_at) VALUES ('01987075-16cb-7337-af15-cd28f64c93a3', 'group', 3, NULL, CURRENT_TIMESTAMP);
INSERT INTO chat.chat(id, type, usergroup_id, container_id, created_at) VALUES ('01987074-1f7f-7aad-ad76-a4b83544fa2d', 'group', 4, NULL, CURRENT_TIMESTAMP);
INSERT INTO chat.chat(id, type, usergroup_id, container_id, created_at) VALUES ('01987074-440c-73f8-aa5b-ba2b50a19395', 'group', 5, NULL, CURRENT_TIMESTAMP);

CREATE TABLE IF NOT EXISTS chat.chat_reads (
    user_id              UUID        NOT NULL,
    chat_id              UUID        NOT NULL REFERENCES chat.chat(id) ON DELETE CASCADE,
    last_read_message_id UUID        REFERENCES chat.message(id) ON DELETE SET NULL,
    last_read_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, chat_id)
);

CREATE INDEX IF NOT EXISTS idx_chat_reads_chat ON chat.chat_reads (chat_id);

