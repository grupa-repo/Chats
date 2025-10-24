package integration_tests

import (
	"testing"
	"time"

	entity "github.com/HappYness-Project/chatApi/internal/message/domain"
	"github.com/HappYness-Project/chatApi/internal/message/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMessageTestData(t *testing.T) {
	// Insert test messages for existing chats
	_, err := testDB.Exec(`
		INSERT INTO public.message(id, chat_id, sender_id, content, message_type, created_at, read_status, deleted_at, deleted_by)
		VALUES
		('01987073-0a87-7b32-9439-86868dfe9bd4', '01987073-0a87-7b32-9439-86868dfe9bd2', '01959b38-b3f9-7ec5-8ac8-e353bfe08a2d', 'Hello everyone!', 'text', '2024-01-01 10:00:00', false, NULL, NULL),
		('01987073-0a87-7b32-9439-86868dfe9bd5', '01987073-0a87-7b32-9439-86868dfe9bd2', '01959b39-febd-770d-9e1b-e5ee392fce54', 'Hi there!', 'text', '2024-01-01 10:01:00', false, NULL, NULL),
		('01987073-0a87-7b32-9439-86868dfe9bd6', '01987073-cf13-7621-af36-54ce20056d18', '0195c388-d0f4-77d5-be90-971d38344c74', 'Private message', 'text', '2024-01-01 10:02:00', true, NULL, NULL)
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)
}

func cleanupMessageTestData(t *testing.T) {
	_, err := testDB.Exec(`
		DELETE FROM public.message
		WHERE id IN (
			'01987073-0a87-7b32-9439-86868dfe9bd4',
			'01987073-0a87-7b32-9439-86868dfe9bd5',
			'01987073-0a87-7b32-9439-86868dfe9bd6'
		)
	`)
	require.NoError(t, err)
}

func TestMessageRepository_DatabaseConnection(t *testing.T) {
	repo := repository.NewRepository(testDB)
	require.NotNil(t, repo)

	err := testDB.Ping()
	require.NoError(t, err)
}

func TestMessageRepository_Create(t *testing.T) {
	repo := repository.NewRepository(testDB)

	t.Run("should create message successfully", func(t *testing.T) {
		// Generate a new UUID for the message
		messageUUID, err := uuid.NewV7()
		require.NoError(t, err)
		messageID := messageUUID.String()

		message := entity.Message{
			ID:          messageID,
			ChatID:      "01987073-0a87-7b32-9439-86868dfe9bd2",
			SenderID:    "01959b38-b3f9-7ec5-8ac8-e353bfe08a2d",
			Content:     "Test message content",
			MessageType: "text",
			CreatedAt:   time.Now(),
		}

		err = repo.Create(message)
		require.NoError(t, err)

		// Verify message was created in database
		var dbMessage entity.Message
		err = testDB.QueryRow(`
			SELECT id, chat_id, sender_id, content, message_type, created_at, read_status, deleted_at, deleted_by
			FROM public.message WHERE id = $1
		`, messageID).Scan(
			&dbMessage.ID, &dbMessage.ChatID, &dbMessage.SenderID,
			&dbMessage.Content, &dbMessage.MessageType, &dbMessage.CreatedAt, &dbMessage.ReadStatus,
			&dbMessage.DeletedAt, &dbMessage.DeletedBy,
		)

		require.NoError(t, err)
		assert.Equal(t, messageID, dbMessage.ID)
		assert.Equal(t, message.ChatID, dbMessage.ChatID)
		assert.Equal(t, message.SenderID, dbMessage.SenderID)
		assert.Equal(t, message.Content, dbMessage.Content)
		assert.Equal(t, message.MessageType, dbMessage.MessageType)
		assert.False(t, dbMessage.ReadStatus) // Should default to false
		assert.False(t, dbMessage.CreatedAt.IsZero())

		// Cleanup
		_, _ = testDB.Exec(`DELETE FROM public.message WHERE id = $1`, messageID)
	})

	t.Run("should handle different message types", func(t *testing.T) {
		messageTypes := []string{"text", "image", "video", "audio", "file"}

		for _, msgType := range messageTypes {
			// Generate a new UUID for each message
			messageUUID, err := uuid.NewV7()
			require.NoError(t, err)
			messageID := messageUUID.String()

			message := entity.Message{
				ID:          messageID,
				ChatID:      "01987073-0a87-7b32-9439-86868dfe9bd2",
				SenderID:    "01959b38-b3f9-7ec5-8ac8-e353bfe08a2d",
				Content:     "Test " + msgType + " message",
				MessageType: msgType,
				CreatedAt:   time.Now(),
			}

			err = repo.Create(message)
			require.NoError(t, err)

			// Verify message type was saved correctly
			var savedType string
			err = testDB.QueryRow(`SELECT message_type FROM public.message WHERE id = $1`, messageID).Scan(&savedType)
			require.NoError(t, err)
			assert.Equal(t, msgType, savedType)

			// Cleanup
			_, _ = testDB.Exec(`DELETE FROM public.message WHERE id = $1`, messageID)
		}
	})

	t.Run("should fail with invalid chat_id", func(t *testing.T) {
		messageUUID, err := uuid.NewV7()
		require.NoError(t, err)
		messageID := messageUUID.String()

		message := entity.Message{
			ID:          messageID,
			ChatID:      "invalid-uuid",
			SenderID:    "01959b38-b3f9-7ec5-8ac8-e353bfe08a2d",
			Content:     "Test message",
			MessageType: "text",
			CreatedAt:   time.Now(),
		}

		err = repo.Create(message)
		require.Error(t, err)
	})
}

func TestMessageRepository_GetByChatID(t *testing.T) {
	setupMessageTestData(t)
	defer cleanupMessageTestData(t)

	repo := repository.NewRepository(testDB)

	t.Run("should return messages for existing chat", func(t *testing.T) {
		chatID := "01987073-0a87-7b32-9439-86868dfe9bd2"

		messages, err := repo.GetByChatID(chatID, 10, 0)

		require.NoError(t, err)
		require.NotNil(t, messages)
		assert.GreaterOrEqual(t, len(messages), 2) // At least our test messages

		// Verify message structure and ordering (should be ASC by created_at)
		for i, msg := range messages {
			assert.NotEmpty(t, msg.ID)
			assert.Equal(t, chatID, msg.ChatID)
			assert.NotEmpty(t, msg.SenderID)
			assert.NotEmpty(t, msg.Content)
			assert.Contains(t, []string{"text", "image", "video", "audio", "file"}, msg.MessageType)
			assert.False(t, msg.CreatedAt.IsZero())

			// Check ordering (created_at ASC)
			if i > 0 {
				assert.True(t, messages[i-1].CreatedAt.Before(msg.CreatedAt) || messages[i-1].CreatedAt.Equal(msg.CreatedAt))
			}
		}
	})

	t.Run("should return empty slice for non-existent chat", func(t *testing.T) {
		nonExistentChatID := "01987073-0000-0000-0000-000000000000"

		messages, err := repo.GetByChatID(nonExistentChatID, 10, 0)

		require.NoError(t, err)
		assert.Equal(t, 0, len(messages))
	})

	t.Run("should respect limit and offset parameters", func(t *testing.T) {
		chatID := "01987073-0a87-7b32-9439-86868dfe9bd2"

		// Get first message only
		messages, err := repo.GetByChatID(chatID, 1, 0)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(messages), 1)

		if len(messages) > 0 {
			firstMessage := messages[0]

			// Get second message with offset
			messagesOffset, err := repo.GetByChatID(chatID, 1, 1)
			require.NoError(t, err)

			if len(messagesOffset) > 0 {
				secondMessage := messagesOffset[0]
				assert.NotEqual(t, firstMessage.ID, secondMessage.ID)
				// Since inner query orders DESC and outer ASC, with offset:
				// - OFFSET 0 gets the most recent message
				// - OFFSET 1 gets the second most recent message
				// So secondMessage should be before firstMessage chronologically
				assert.True(t, secondMessage.CreatedAt.Before(firstMessage.CreatedAt) || secondMessage.CreatedAt.Equal(firstMessage.CreatedAt))
			}
		}
	})
}

func TestMessageRepository_MessagePersistence(t *testing.T) {
	repo := repository.NewRepository(testDB)

	t.Run("should persist and retrieve message with all fields", func(t *testing.T) {
		// Create a message
		messageUUID, err := uuid.NewV7()
		require.NoError(t, err)
		messageID := messageUUID.String()

		originalMessage := entity.Message{
			ID:          messageID,
			ChatID:      "01987073-0a87-7b32-9439-86868dfe9bd2",
			SenderID:    "01959b38-b3f9-7ec5-8ac8-e353bfe08a2d",
			Content:     "Test persistence message with special chars: 안녕하세요 여러분 🎉",
			MessageType: "text",
			CreatedAt:   time.Now().Truncate(time.Microsecond), // Truncate to match DB precision
		}

		err = repo.Create(originalMessage)
		require.NoError(t, err)

		// Retrieve it back
		messages, err := repo.GetByChatID(originalMessage.ChatID, 100, 0)
		require.NoError(t, err)

		var foundMessage *entity.Message
		for _, msg := range messages {
			if msg.ID == messageID {
				foundMessage = &msg
				break
			}
		}

		require.NotNil(t, foundMessage, "Created message should be found")
		assert.Equal(t, originalMessage.ID, foundMessage.ID)
		assert.Equal(t, originalMessage.ChatID, foundMessage.ChatID)
		assert.Equal(t, originalMessage.SenderID, foundMessage.SenderID)
		assert.Equal(t, originalMessage.Content, foundMessage.Content)
		assert.Equal(t, originalMessage.MessageType, foundMessage.MessageType)
		assert.False(t, foundMessage.ReadStatus) // Should default to false
		assert.True(t, originalMessage.CreatedAt.Equal(foundMessage.CreatedAt.Truncate(time.Microsecond)))

		// Cleanup
		_, _ = testDB.Exec(`DELETE FROM public.message WHERE id = $1`, messageID)
	})
}
