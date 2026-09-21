package dev.kerti.afloat.config

import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import tools.jackson.core.JsonGenerator
import tools.jackson.databind.JacksonModule
import tools.jackson.databind.SerializationContext
import tools.jackson.databind.ValueSerializer
import tools.jackson.databind.module.SimpleModule
import java.math.BigDecimal

// Money is DECIMAL(20,4) and goes on the wire as a STRING (BOOTSTRAP.md §4,
// CLAUDE.md non-negotiable 1). Both halves of that matter and they are
// different bugs: WRITE_BIGDECIMAL_AS_PLAIN alone still emits a JSON number,
// and converting to a string without it emits "2.5E+7".
//
// Global, not per-field, per docs/adr/kotlin/0001 rule 6 and issue #13 §3.6:
// per-field annotations mean every future money column is another chance to
// forget, and the one that gets forgotten is found by a frontend parsing
// 2.5E+7 as a budget.
//
// Nothing on the wire reaches this today — every money field in the contract is
// declared `type: string`, so the generated models carry String and the service
// converts with toPlainString(). That is precisely why the configuration is
// asserted rather than assumed: the first hand-written DTO or the first
// generator change that yields a BigDecimal property must land on the right
// side of this, not discover it in review.
@Configuration
class MoneySerializationConfiguration {

    @Bean
    fun moneyModule(): JacksonModule =
        SimpleModule("afloat-money").addSerializer(BigDecimal::class.java, PlainStringBigDecimalSerializer())
}

class PlainStringBigDecimalSerializer : ValueSerializer<BigDecimal>() {
    // toPlainString, not toString: toString switches to scientific notation
    // once the exponent is large enough, which is the exact footgun
    // BOOTSTRAP.md §4 names. It also preserves the scale the column carries, so
    // a DECIMAL(20,4) of 1.5 stays "1.5000" rather than becoming "1.5".
    override fun serialize(value: BigDecimal, gen: JsonGenerator, ctxt: SerializationContext) {
        gen.writeString(value.toPlainString())
    }
}
