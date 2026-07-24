# Colsubsidio Protege API Documentation Analysis

## 1. Introduction

This report provides a comprehensive analysis of the Colsubsidio Protege API documentation, based on the OpenAPI specification found at `http://147.93.11.136:9000/openapi.json`.

## 2. API Overview

### API Title: Colsubsidio Protege API
### API Version: 0.1.0

The API is designed to manage various aspects related to users, variables, products, rules, recommendations, questions, and conversations.

## 3. API Endpoints

The API is organized into several functional areas, each exposed through a set of endpoints. Below is a summary of the main functional areas and their respective endpoints.


### 3.1. Users

The `users` endpoints allow for the management and retrieval of user information. This includes creating new users, searching for existing users, retrieving specific user details, and updating user records.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| POST | `/api/v1/users` | Create | `create_api_v1_users_post` | `UserCreate` | None | `201: UserRead`, `422: HTTPValidationError` |
| GET | `/api/v1/users/search` | Search | `search_api_v1_users_search_get` | None | `phone`, `document_number`, `nit`, `email` (query) | `200: UserRead[]`, `422: HTTPValidationError` |
| GET | `/api/v1/users/{user_id}` | Get | `get_api_v1_users__user_id__get` | None | `user_id` (path) | `200: UserRead`, `422: HTTPValidationError` |
| PATCH | `/api/v1/users/{user_id}` | Update | `update_api_v1_users__user_id__patch` | `UserUpdate` | `user_id` (path) | `200: UserRead`, `422: HTTPValidationError` |


### 3.2. Variables

The `variables` endpoints handle the definition and management of variables associated with users. This includes listing and creating variable definitions, and saving or retrieving user-specific variable values.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| GET | `/api/v1/variable-definitions` | List Definitions | `list_definitions_api_v1_variable_definitions_get` | None | None | `200: VariableDefinitionRead[]` |
| POST | `/api/v1/variable-definitions` | Create Definition | `create_definition_api_v1_variable_definitions_post` | `VariableDefinitionCreate` | None | `201: VariableDefinitionRead`, `422: HTTPValidationError` |
| PUT | `/api/v1/users/{user_id}/variables` | Save Variables | `save_variables_api_v1_users__user_id__variables_put` | `VariableValueInput[]` | `user_id` (path) | `200: UserVariableRead[]`, `422: HTTPValidationError` |
| GET | `/api/v1/users/{user_id}/variables` | Get Variables | `get_api_v1_users__user_id__variables_get` | None | `user_id` (path) | `200: UserVariableRead[]`, `422: HTTPValidationError` |


### 3.3. Products

The `products` endpoints allow for the creation and listing of products within the system.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| POST | `/api/v1/products` | Create | `create_api_v1_products_post` | `ProductCreate` | None | `201: ProductRead`, `422: HTTPValidationError` |
| GET | `/api/v1/products` | List All | `list_all_api_v1_products_get` | None | `active` (query) | `200: ProductRead[]`, `422: HTTPValidationError` |


### 3.4. Rules

The `rules` endpoints are used for creating, listing, and updating rules within the system.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| POST | `/api/v1/rules` | Create | `create_api_v1_rules_post` | `RuleCreate` | None | `201: RuleRead`, `422: HTTPValidationError` |
| GET | `/api/v1/rules` | List All | `list_all_api_v1_rules_get` | None | `product_id` (query) | `200: RuleRead[]`, `422: HTTPValidationError` |
| PATCH | `/api/v1/rules/{rule_id}` | Update | `update_api_v1_rules__rule_id__patch` | `RuleUpdate` | `rule_id` (path) | `200: RuleRead`, `422: HTTPValidationError` |


### 3.5. Recommendations

The `recommendations` endpoint is used to generate recommendations for a specific user.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| POST | `/api/v1/recommendations/users/{user_id}` | Generate | `generate_api_v1_recommendations_users__user_id__post` | None | `user_id` (path), `limit` (query) | `200: {}`, `422: HTTPValidationError` |


### 3.6. Questions

The `questions` endpoints allow for the creation, listing, retrieval, and updating of questions, as well as the creation of conditions for questions.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| POST | `/api/v1/questions` | Create | `create_api_v1_questions_post` | `QuestionCreate` | None | `201: QuestionRead`, `422: HTTPValidationError` |
| GET | `/api/v1/questions` | List All | `list_all_api_v1_questions_get` | None | `active` (query) | `200: QuestionRead[]`, `422: HTTPValidationError` |
| GET | `/api/v1/questions/{question_id}` | Get One | `get_one_api_v1_questions__question_id__get` | None | `question_id` (path) | `200: QuestionRead`, `422: HTTPValidationError` |
| PATCH | `/api/v1/questions/{question_id}` | Update | `update_api_v1_questions__question_id__patch` | `QuestionUpdate` | `question_id` (path) | `200: QuestionRead`, `422: HTTPValidationError` |
| POST | `/api/v1/questions/{question_id}/conditions` | Create Condition | `create_condition_api_v1_questions__question_id__conditions_post` | `QuestionConditionCreate` | `question_id` (path) | `201: QuestionRead`, `422: HTTPValidationError` |


### 3.7. Conversations

The `conversations` endpoints manage the lifecycle of user conversations, including creation, retrieval, answering questions within a conversation, and completing a conversation to generate recommendations.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| POST | `/api/v1/conversations` | Create | `create_api_v1_conversations_post` | `ConversationCreate` | None | `201: ConversationRead`, `422: HTTPValidationError` |
| GET | `/api/v1/conversations/{conversation_id}` | Get One | `get_one_api_v1_conversations__conversation_id__get` | None | `conversation_id` (path) | `200: ConversationRead`, `422: HTTPValidationError` |
| POST | `/api/v1/conversations/{conversation_id}/answers` | Answer | `answer_api_v1_conversations__conversation_id__answers_post` | `ConversationAnswerCreate` | `conversation_id` (path) | `200: ConversationAnswerResponse`, `422: HTTPValidationError` |
| POST | `/api/v1/conversations/{conversation_id}/complete` | Complete | `complete_api_v1_conversations__conversation_id__complete_post` | None | `conversation_id` (path), `limit` (query) | `200: ConversationCompleteResponse`, `422: HTTPValidationError` |


### 3.8. Health

The `health` endpoint provides a simple way to check the API's operational status.

| Method | Path | Summary | Operation ID | Request Body | Parameters | Responses |
|---|---|---|---|---|---|---|
| GET | `/health` | Health | `health_health_get` | None | None | `200: {}` |

## 4. API Schemas (Data Models)

The following are the data models (schemas) used across the API, defining the structure of request and response bodies.


### ConditionOperator

**Type:** `string`

**Enum Values:** equals, not_equals, gt, gte, lt, lte, in, contains, exists

### ConversationAnswerCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `question_id` | `string` | `uuid` | Yes | Question Id |
| `value` | `` | `-` | Yes | Value |
| `source` | `string` | `-` | No | Source |

### ConversationAnswerResponse

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `id` | `string` | `uuid` | Yes | Id |
| `user_id` | `string` | `uuid` | Yes | User Id |
| `channel` | `string` | `-` | Yes | Channel |
| `status` | `string` | `-` | Yes | Status |
| `current_question_id` | `string or null` | `-` | Yes | Current Question Id |
| `external_session_id` | `string or null` | `-` | Yes | External Session Id |
| `context` | `object` | `-` | Yes | Context |
| `completed_at` | `string or null` | `-` | Yes | Completed At |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |
| `next_question` | `Ref: QuestionPublic or null` | `-` | Yes | - |
| `can_generate_recommendation` | `boolean` | `-` | Yes | Can Generate Recommendation |
| `saved_answer` | `Reference: `SavedAnswer`` | `-` | Yes | - |

### ConversationChannel

**Type:** `string`

**Enum Values:** web, whatsapp, voice, api

### ConversationCompleteResponse

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `conversation_id` | `string` | `uuid` | Yes | Conversation Id |
| `status` | `Reference: `ConversationStatus`` | `-` | Yes | - |
| `snapshot_id` | `string` | `uuid` | Yes | Snapshot Id |
| `user_id` | `string` | `uuid` | Yes | User Id |
| `recommendations` | `array` | `-` | Yes | Recommendations |

### ConversationCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `user_id` | `string` | `uuid` | Yes | User Id |
| `channel` | `Reference: `ConversationChannel`` | `-` | Yes | - |
| `external_session_id` | `string or null` | `-` | No | External Session Id |
| `context` | `object` | `-` | No | Context |

### ConversationRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `id` | `string` | `uuid` | Yes | Id |
| `user_id` | `string` | `uuid` | Yes | User Id |
| `channel` | `string` | `-` | Yes | Channel |
| `status` | `string` | `-` | Yes | Status |
| `current_question_id` | `string or null` | `-` | Yes | Current Question Id |
| `external_session_id` | `string or null` | `-` | Yes | External Session Id |
| `context` | `object` | `-` | Yes | Context |
| `completed_at` | `string or null` | `-` | Yes | Completed At |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |
| `next_question` | `Ref: QuestionPublic or null` | `-` | Yes | - |
| `can_generate_recommendation` | `boolean` | `-` | Yes | Can Generate Recommendation |

### ConversationStatus

**Type:** `string`

**Enum Values:** new, collecting_data, ready_for_recommendation, completed, cancelled

### FieldType

**Type:** `string`

**Enum Values:** text, textarea, number, currency, boolean, radio, select, multi_select, date, email, phone

### HTTPValidationError

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `detail` | `array` | `-` | No | Detail |

### ProductCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `code` | `string` | `-` | Yes | Code |
| `name` | `string` | `-` | Yes | Name |
| `description` | `string or null` | `-` | No | Description |
| `category` | `string` | `-` | Yes | Category |
| `active` | `boolean` | `-` | No | Active |
| `base_price` | `number or null` | `-` | No | Base Price |
| `metadata_json` | `object` | `-` | No | Metadata Json |

### ProductRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `code` | `string` | `-` | Yes | Code |
| `name` | `string` | `-` | Yes | Name |
| `description` | `string or null` | `-` | No | Description |
| `category` | `string` | `-` | Yes | Category |
| `active` | `boolean` | `-` | No | Active |
| `base_price` | `number or null` | `-` | No | Base Price |
| `metadata_json` | `object` | `-` | No | Metadata Json |
| `id` | `string` | `uuid` | Yes | Id |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |

### QuestionConditionCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `depends_on_variable_key` | `string` | `-` | Yes | Depends On Variable Key |
| `operator` | `Reference: `ConditionOperator`` | `-` | Yes | - |
| `expected_value` | `null` | `-` | No | Expected Value |

### QuestionConditionRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `depends_on_variable_key` | `string` | `-` | Yes | Depends On Variable Key |
| `operator` | `Reference: `ConditionOperator`` | `-` | Yes | - |
| `expected_value` | `null` | `-` | No | Expected Value |
| `id` | `string` | `uuid` | Yes | Id |
| `question_id` | `string` | `uuid` | Yes | Question Id |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |

### QuestionCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `variable_key` | `string` | `-` | Yes | Variable Key |
| `text` | `string` | `-` | Yes | Text |
| `field_type` | `Reference: `FieldType`` | `-` | Yes | - |
| `required` | `boolean` | `-` | No | Required |
| `order_index` | `integer` | `-` | No | Order Index |
| `active` | `boolean` | `-` | No | Active |
| `help_text` | `string or null` | `-` | No | Help Text |
| `placeholder` | `string or null` | `-` | No | Placeholder |
| `validation` | `object` | `-` | No | Validation |
| `options` | `array` | `-` | No | Options |
| `conditions` | `array` | `-` | No | Conditions |

### QuestionPublic

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `id` | `string` | `uuid` | Yes | Id |
| `variable_key` | `string` | `-` | Yes | Variable Key |
| `text` | `string` | `-` | Yes | Text |
| `field_type` | `string` | `-` | Yes | Field Type |
| `required` | `boolean` | `-` | Yes | Required |
| `help_text` | `string or null` | `-` | Yes | Help Text |
| `placeholder` | `string or null` | `-` | Yes | Placeholder |
| `validation` | `object` | `-` | Yes | Validation |
| `options` | `array` | `-` | Yes | Options |

### QuestionRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `id` | `string` | `uuid` | Yes | Id |
| `variable_definition_id` | `string` | `uuid` | Yes | Variable Definition Id |
| `variable_key` | `string` | `-` | Yes | Variable Key |
| `text` | `string` | `-` | Yes | Text |
| `field_type` | `string` | `-` | Yes | Field Type |
| `required` | `boolean` | `-` | Yes | Required |
| `order_index` | `integer` | `-` | Yes | Order Index |
| `active` | `boolean` | `-` | Yes | Active |
| `help_text` | `string or null` | `-` | Yes | Help Text |
| `placeholder` | `string or null` | `-` | Yes | Placeholder |
| `validation` | `object` | `-` | Yes | Validation |
| `options` | `array` | `-` | Yes | Options |
| `conditions` | `array` | `-` | Yes | Conditions |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |

### QuestionUpdate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `text` | `string or null` | `-` | No | Text |
| `field_type` | `Ref: FieldType or null` | `-` | No | - |
| `required` | `boolean or null` | `-` | No | Required |
| `order_index` | `integer or null` | `-` | No | Order Index |
| `active` | `boolean or null` | `-` | No | Active |
| `help_text` | `string or null` | `-` | No | Help Text |
| `placeholder` | `string or null` | `-` | No | Placeholder |
| `validation` | `object or null` | `-` | No | Validation |
| `options` | `array or null` | `-` | No | Options |

### RuleCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `product_id` | `string` | `uuid` | Yes | Product Id |
| `name` | `string` | `-` | Yes | Name |
| `variable_key` | `string` | `-` | Yes | Variable Key |
| `operator` | `string` | `-` | Yes | Operator |
| `expected_value` | `` | `-` | Yes | Expected Value |
| `weight` | `number` | `-` | No | Weight |
| `reason` | `string` | `-` | Yes | Reason |
| `active` | `boolean` | `-` | No | Active |

### RuleRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `product_id` | `string` | `uuid` | Yes | Product Id |
| `name` | `string` | `-` | Yes | Name |
| `variable_key` | `string` | `-` | Yes | Variable Key |
| `operator` | `string` | `-` | Yes | Operator |
| `expected_value` | `` | `-` | Yes | Expected Value |
| `weight` | `number` | `-` | No | Weight |
| `reason` | `string` | `-` | Yes | Reason |
| `active` | `boolean` | `-` | No | Active |
| `id` | `string` | `uuid` | Yes | Id |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |

### RuleUpdate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `name` | `string or null` | `-` | No | Name |
| `operator` | `string or null` | `-` | No | Operator |
| `expected_value` | `null` | `-` | No | Expected Value |
| `weight` | `number or null` | `-` | No | Weight |
| `reason` | `string or null` | `-` | No | Reason |
| `active` | `boolean or null` | `-` | No | Active |

### SavedAnswer

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `question_id` | `string` | `uuid` | Yes | Question Id |
| `variable_key` | `string` | `-` | Yes | Variable Key |
| `value` | `` | `-` | Yes | Value |
| `source` | `string` | `-` | Yes | Source |

### UserCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `document_type` | `string or null` | `-` | No | Document Type |
| `document_number` | `string or null` | `-` | No | Document Number |
| `nit` | `string or null` | `-` | No | Nit |
| `phone` | `string or null` | `-` | No | Phone |
| `email` | `string or null` | `-` | No | Email |
| `first_name` | `string or null` | `-` | No | First Name |
| `last_name` | `string or null` | `-` | No | Last Name |

### UserRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `id` | `string` | `uuid` | Yes | Id |
| `document_type` | `string or null` | `-` | Yes | Document Type |
| `document_number` | `string or null` | `-` | Yes | Document Number |
| `nit` | `string or null` | `-` | Yes | Nit |
| `phone` | `string or null` | `-` | Yes | Phone |
| `email` | `string or null` | `-` | Yes | Email |
| `first_name` | `string or null` | `-` | Yes | First Name |
| `last_name` | `string or null` | `-` | Yes | Last Name |
| `is_active` | `boolean` | `-` | Yes | Is Active |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |

### UserUpdate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `phone` | `string or null` | `-` | No | Phone |
| `email` | `string or null` | `-` | No | Email |
| `first_name` | `string or null` | `-` | No | First Name |
| `last_name` | `string or null` | `-` | No | Last Name |
| `is_active` | `boolean or null` | `-` | No | Is Active |

### UserVariableRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `key` | `string` | `-` | Yes | Key |
| `value` | `` | `-` | Yes | Value |
| `source` | `string` | `-` | Yes | Source |
| `confidence` | `number or null` | `-` | Yes | Confidence |

### ValidationError

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `loc` | `array` | `-` | Yes | Location |
| `msg` | `string` | `-` | Yes | Message |
| `type` | `string` | `-` | Yes | Error Type |
| `input` | `` | `-` | No | Input |
| `ctx` | `object` | `-` | No | Context |

### VariableDefinitionCreate

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `key` | `string` | `-` | Yes | Key |
| `label` | `string` | `-` | Yes | Label |
| `data_type` | `string` | `-` | Yes | Data Type |
| `required` | `boolean` | `-` | No | Required |
| `active` | `boolean` | `-` | No | Active |
| `validation` | `object` | `-` | No | Validation |
| `question` | `string or null` | `-` | No | Question |
| `options` | `array` | `-` | No | Options |

### VariableDefinitionRead

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `key` | `string` | `-` | Yes | Key |
| `label` | `string` | `-` | Yes | Label |
| `data_type` | `string` | `-` | Yes | Data Type |
| `required` | `boolean` | `-` | No | Required |
| `active` | `boolean` | `-` | No | Active |
| `validation` | `object` | `-` | No | Validation |
| `question` | `string or null` | `-` | No | Question |
| `options` | `array` | `-` | No | Options |
| `id` | `string` | `uuid` | Yes | Id |
| `created_at` | `string` | `date-time` | Yes | Created At |
| `updated_at` | `string` | `date-time` | Yes | Updated At |

### VariableValueInput

**Type:** `object`

| Property | Type | Format | Required | Description |
|---|---|---|---|---|
| `key` | `string` | `-` | Yes | Key |
| `value` | `` | `-` | Yes | Value |
| `source` | `string` | `-` | No | Source |
| `confidence` | `number or null` | `-` | No | Confidence |

