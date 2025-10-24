package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type ChatType string

const (
	ChatTypePrivate   ChatType = "private"
	ChatTypeGroup     ChatType = "group"
	ChatTypeContainer ChatType = "container"
)

func (ct ChatType) String() string {
	return string(ct)
}

func (ct ChatType) IsValid() bool {
	switch ct {
	case ChatTypePrivate, ChatTypeGroup, ChatTypeContainer:
		return true
	default:
		return false
	}
}

type Chat struct {
	Id          uuid.UUID  `json:"id"`
	Type        ChatType   `json:"type"`
	UserGroupId *int       `json:"usergroup_id,omitempty"`
	ContainerId *uuid.UUID `json:"container_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func NewChat(chatType ChatType, userGroupId *int, containerId *uuid.UUID) (*Chat, error) {
	if !chatType.IsValid() {
		return nil, errors.New("invalid chat type")
	}

	if err := validateChatConfiguration(chatType, userGroupId, containerId); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	return &Chat{
		Id:          id,
		Type:        chatType,
		UserGroupId: userGroupId,
		ContainerId: containerId,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func validateChatConfiguration(chatType ChatType, userGroupId *int, containerId *uuid.UUID) error {
	switch chatType {
	case ChatTypeGroup:
		if userGroupId == nil {
			return errors.New("group chats require a user group ID")
		}
		if containerId != nil {
			return errors.New("group chats cannot have a container ID")
		}
	case ChatTypeContainer:
		if containerId == nil {
			return errors.New("container chats require a container ID")
		}
		if userGroupId != nil {
			return errors.New("container chats cannot have a user group ID")
		}
	case ChatTypePrivate:
		if userGroupId != nil || containerId != nil {
			return errors.New("private chats cannot have user group ID or container ID")
		}
	}
	return nil
}

func (c *Chat) IsGroup() bool {
	return c.Type == ChatTypeGroup
}

func (c *Chat) IsPrivate() bool {
	return c.Type == ChatTypePrivate
}

func (c *Chat) IsContainer() bool {
	return c.Type == ChatTypeContainer
}
