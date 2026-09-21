package dev.kerti.afloat.httperr

import dev.kerti.afloat.api.model.ErrorCode

class ApiException(
    val status: Int,
    val code: ErrorCode,
    val args: Map<String, Any>? = null,
    val retryAfterSeconds: Int? = null,
) : RuntimeException(code.value)
