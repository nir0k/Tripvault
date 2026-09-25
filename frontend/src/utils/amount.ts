/**
 * Amounts may be typed as a formula the way a spreadsheet takes one: a leading
 * "=" followed by numbers, the four operators and parentheses, "=120*3+45".
 * Only the result is ever stored; the formula is a way of typing it.
 */

/** isFormula reports whether a typed amount is a formula rather than a number. */
export function isFormula(value: string): boolean {
  return value.trimStart().startsWith('=')
}

/**
 * evaluateFormula computes a formula, with or without its leading "=". A
 * decimal comma is read as a point and spaces are ignored, as in a plain
 * amount. It returns null for anything that is not a well-formed expression of
 * numbers, + - * / and parentheses, and for a division by zero.
 */
export function evaluateFormula(value: string): number | null {
  const source = value.trim().replace(/^=/, '').replace(/\s/g, '').replace(/,/g, '.')
  let index = 0

  // expression := term (("+" | "-") term)*
  function expression(): number | null {
    let result = term()
    while (result !== null && (source[index] === '+' || source[index] === '-')) {
      const operator = source[index++]
      const right = term()
      if (right === null) {
        return null
      }
      result = operator === '+' ? result + right : result - right
    }
    return result
  }

  // term := factor (("*" | "/") factor)*
  function term(): number | null {
    let result = factor()
    while (result !== null && (source[index] === '*' || source[index] === '/')) {
      const operator = source[index++]
      const right = factor()
      if (right === null || (operator === '/' && right === 0)) {
        return null
      }
      result = operator === '*' ? result * right : result / right
    }
    return result
  }

  // factor := "-" factor | "(" expression ")" | number
  function factor(): number | null {
    if (source[index] === '-') {
      index++
      const inner = factor()
      return inner === null ? null : -inner
    }
    if (source[index] === '(') {
      index++
      const inner = expression()
      if (inner === null || source[index] !== ')') {
        return null
      }
      index++
      return inner
    }
    const match = /^\d+(\.\d+)?|^\.\d+/.exec(source.slice(index))
    if (!match) {
      return null
    }
    index += match[0].length
    return Number(match[0])
  }

  if (source === '') {
    return null
  }
  const result = expression()
  return result !== null && index === source.length && Number.isFinite(result) ? result : null
}

/**
 * resolveAmount turns what was typed into an amount into the plain number it
 * stands for, rounded to cents: a formula is computed, anything else is
 * returned unchanged. A formula that cannot be computed or comes out negative
 * - no cost is negative - is returned unchanged too, and invalidAmount reports it.
 */
export function resolveAmount(value: string): string {
  if (!isFormula(value)) {
    return value
  }
  const result = evaluateFormula(value)
  if (result === null || result < 0) {
    return value
  }
  return String(Math.round(result * 100) / 100)
}

/** invalidAmount reports a formula that does not give an amount. */
export function invalidAmount(value: string): boolean {
  return isFormula(value) && isFormula(resolveAmount(value))
}
