package dev.kerti.afloat.auth

import dev.kerti.afloat.api.model.ErrorCode
import dev.kerti.afloat.httperr.ApiErrorWriter
import jakarta.servlet.FilterChain
import jakarta.servlet.ReadListener
import jakarta.servlet.ServletInputStream
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletRequestWrapper
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.HttpHeaders
import org.springframework.web.filter.OncePerRequestFilter
import java.io.BufferedReader
import java.io.IOException
import java.io.InputStreamReader

class MaxBodyFilter : OncePerRequestFilter() {
    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        val declared = request.getHeader(HttpHeaders.CONTENT_LENGTH)?.toLongOrNull()
        if (declared != null && declared > MAX_BODY_BYTES) {
            ApiErrorWriter.write(response, 400, ErrorCode.INVALID_JSON_BODY)
            return
        }
        // Content-Length is absent on a chunked body, so the stream itself is
        // capped too: Go's MaxBytesReader counts bytes, not headers.
        filterChain.doFilter(LimitedBodyRequest(request, MAX_BODY_BYTES), response)
    }

    companion object {
        private const val MAX_BODY_BYTES = 1L shl 20 // 1 MiB, identical to Go
    }
}

private class LimitedBodyRequest(
    request: HttpServletRequest,
    private val limit: Long,
) : HttpServletRequestWrapper(request) {

    // The counter lives on the wrapper, not on the stream object: getReader()
    // goes through getInputStream(), and a per-stream counter would hand the
    // second caller a fresh allowance for the same body.
    private var count = 0L
    private var stream: ServletInputStream? = null

    override fun getInputStream(): ServletInputStream {
        stream?.let { return it }
        val delegate = super.getInputStream()
        val limited = object : ServletInputStream() {
            override fun read(): Int {
                val b = delegate.read()
                if (b != -1) consume(1)
                return b
            }

            // Overridden, not inherited. InputStream's default bulk read loops
            // on read() one byte at a time, so leaving it out does not just
            // lose the fast path — it puts a virtual call per byte under every
            // JSON parse in the application, oversized or not.
            override fun read(b: ByteArray, off: Int, len: Int): Int {
                val read = delegate.read(b, off, len)
                if (read > 0) consume(read.toLong())
                return read
            }

            override fun isFinished(): Boolean = delegate.isFinished

            override fun isReady(): Boolean = delegate.isReady

            override fun setReadListener(listener: ReadListener?) = delegate.setReadListener(listener)
        }
        stream = limited
        return limited
    }

    private fun consume(bytes: Long) {
        count += bytes
        if (count > limit) throw IOException("request body exceeds $limit bytes")
    }

    // Routed through the capped stream as well, or reading the body as text
    // would walk straight past the limit.
    override fun getReader(): BufferedReader =
        BufferedReader(InputStreamReader(inputStream, characterEncoding ?: Charsets.UTF_8.name()))
}
