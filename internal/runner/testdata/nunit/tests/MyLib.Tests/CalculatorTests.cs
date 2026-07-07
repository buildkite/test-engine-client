using NUnit.Framework;

namespace MyLib.Tests
{
    public class CalculatorTests
    {
        [Test]
        public void AddTwoNumbers()
        {
            Assert.That(1 + 1, Is.EqualTo(2));
        }
    }
}
